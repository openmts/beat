package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/store"
)

func consumeBackupEnrollment(t *testing.T, fixture *backupFixture, id string) agentjoin.Result {
	t.Helper()
	var hash []byte
	if err := fixture.sqlite.DB.QueryRow("SELECT token_hash FROM agent_enrollment_tokens WHERE id=?", id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.NewNodeStore(fixture.sqlite.DB).ConsumeEnrollmentToken(t.Context(), store.EnrollmentConsumeInput{
		TokenHash: hash, SourceIP: "192.0.2.10", Secure: true, Request: agentjoin.Request{
			RequestID: "backup-client", AgentToken: credential.Plaintext, NodeName: "backup-node", SSHPort: 22,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// 构造旧发行版本可能生成的合法归档：其中接入凭据仍未失效。
func legacyEnrollmentArchive(t *testing.T, fixture *backupFixture, dataDir string) string {
	t.Helper()
	workspace := t.TempDir()
	if err := fixture.sqlite.Snapshot(t.Context(), filepath.Join(workspace, sqliteEntry)); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	rows, err := fixture.service.writeMetrics(t.Context(), filepath.Join(workspace, metricsEntry), cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.copyDataKey(workspace); err != nil {
		t.Fatal(err)
	}
	payloads := []string{sqliteEntry, metricsEntry, dataKeyEntry}
	manifest, err := fixture.service.buildManifest(workspace, payloads, cutoff, rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMetadataFiles(workspace, manifest); err != nil {
		t.Fatal(err)
	}
	archiveRoot := filepath.Join(dataDir, "backups")
	if err := os.Mkdir(archiveRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveRoot, "legacy.zip")
	if err := writeArchive(archivePath+".partial", archivePath, workspace, payloads); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func TestRestoreInvalidatesLegacyGrantsAndRetainsIdentityBindings(t *testing.T) {
	fixture := newBackupFixture(t)
	id := issueBackupEnrollment(t, fixture)
	joined := consumeBackupEnrollment(t, fixture, id)
	dataDir := t.TempDir()
	archive := legacyEnrollmentArchive(t, fixture, dataDir)
	if err := writeJournal(filepath.Join(dataDir, "restore.pending.json"), restoreJournal("legacy", archive)); err != nil {
		t.Fatal(err)
	}
	dbPath, mtsPath := filepath.Join(dataDir, "beat.db"), filepath.Join(dataDir, "beat_mts")
	if err := ApplyPending(t.Context(), dataDir, dbPath, mtsPath); err != nil {
		t.Fatal(err)
	}
	if !snapshotEnrollmentRevoked(t, dbPath, id) {
		t.Fatal("restoration revived archived enrollment token")
	}
	if snapshotEnrollmentRevoked(t, filepath.Join(fixture.dataDir, "beat.db"), id) {
		t.Fatal("restore invalidated original live token")
	}
	restored, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	var count int
	if err := restored.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_identities WHERE node_id=?", joined.NodeID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("identity binding lost: %v", err)
	}
	node, err := store.NewNodeStore(restored.DB).AuthenticateAgentToken(t.Context(), joined.Config.AgentToken)
	if err != nil || node == nil || node.ID != joined.NodeID {
		t.Fatalf("permanent credential did not survive restore: %v", err)
	}
}

func TestRestoreInvalidationFailurePreservesExistingDatabase(t *testing.T) {
	fixture := newBackupFixture(t)
	issueBackupEnrollment(t, fixture)
	if _, err := fixture.sqlite.DB.Exec(`CREATE TRIGGER deny_restore_invalidation BEFORE UPDATE ON agent_enrollment_tokens BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	archive := legacyEnrollmentArchive(t, fixture, dataDir)
	dbPath, mtsPath := filepath.Join(dataDir, "beat.db"), filepath.Join(dataDir, "beat_mts")
	live, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := store.NewNodeStore(live.DB).UpsertNode(t.Context(), "original-node", "192.0.2.4", 22)
	closeErr := live.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("prepare original database: %v/%v", writeErr, closeErr)
	}
	if err := writeJournal(filepath.Join(dataDir, "restore.pending.json"), restoreJournal("legacy", archive)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(t.Context(), dataDir, dbPath, mtsPath); err == nil {
		t.Fatal("restore ignored invalidation failure")
	}
	preserved, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = preserved.Close() }()
	node, err := store.NewNodeStore(preserved.DB).GetNodeByName(t.Context(), "original-node")
	if err != nil || node == nil {
		t.Fatalf("failed restore replaced original database: %v", err)
	}
}
