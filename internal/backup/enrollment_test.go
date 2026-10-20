package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/store"
)

func issueBackupEnrollment(t *testing.T, fixture *backupFixture) string {
	t.Helper()
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	issued, err := store.NewNodeStore(fixture.sqlite.DB).CreateEnrollmentToken(t.Context(), store.EnrollmentTokenInput{
		Hash: token.Hash, Prefix: token.Prefix, ServerURL: "https://beat.example", CreatorID: "owner", CreatedAt: now, ExpiresAt: now.Add(agentjoin.TokenLifetime),
	})
	if err != nil {
		t.Fatal(err)
	}
	return issued.ID
}

func snapshotEnrollmentRevoked(t *testing.T, path, id string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var revoked sql.NullTime
	if err := db.QueryRow("SELECT revoked_at FROM agent_enrollment_tokens WHERE id = ?", id).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	return revoked.Valid
}

func TestBackupInvalidatesEnrollmentOnlyInPrivateSnapshot(t *testing.T) {
	fixture := newBackupFixture(t)
	id := issueBackupEnrollment(t, fixture)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	path := filepath.Join(fixture.dataDir, "backups", record.Filename)
	validated, err := validateArchive(t.Context(), path, fixture.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	if !snapshotEnrollmentRevoked(t, filepath.Join(validated.root, sqliteEntry), id) {
		t.Fatal("backup retained active enrollment credential")
	}
	if snapshotEnrollmentRevoked(t, filepath.Join(fixture.dataDir, "beat.db"), id) {
		t.Fatal("backup revoked live enrollment credential")
	}
}

func TestInvalidateEnrollmentSnapshotErrorsAndOldSchemas(t *testing.T) {
	fixture := newBackupFixture(t)
	id := issueBackupEnrollment(t, fixture)
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if err := invalidateEnrollmentCredentials(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if !snapshotEnrollmentRevoked(t, path, id) {
		t.Fatal("snapshot token was not revoked")
	}
	if err := invalidateEnrollmentCredentials(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE agent_enrollment_tokens"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := invalidateEnrollmentCredentials(t.Context(), path); err != nil {
		t.Fatalf("old snapshot: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := invalidateEnrollmentCredentials(ctx, path); err == nil {
		t.Fatal("canceled invalidation succeeded")
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	if err := invalidateEnrollmentCredentials(t.Context(), missing); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing snapshot was created")
	}
}

func TestInvalidationFailurePreventsBackupPublication(t *testing.T) {
	fixture := newBackupFixture(t)
	issueBackupEnrollment(t, fixture)
	if _, err := fixture.sqlite.DB.Exec(`CREATE TRIGGER reject_enrollment_invalidation BEFORE UPDATE ON agent_enrollment_tokens BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != "failed" {
		t.Fatalf("unsafe backup published: %v", err)
	}
}

func TestEnrollmentInvalidationRejectsBusyCheckpoint(t *testing.T) {
	fixture := newBackupFixture(t)
	issueBackupEnrollment(t, fixture)
	path := filepath.Join(t.TempDir(), "busy.sqlite")
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := reader.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	transaction, err := reader.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	var count int
	if err := transaction.QueryRow("SELECT COUNT(*) FROM agent_enrollment_tokens").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := invalidateEnrollmentCredentials(t.Context(), path); err == nil {
		t.Fatal("busy snapshot checkpoint was accepted")
	}
}
