package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestReconcileQuarantinesOrphansAndRemovesGeneratedPartials(t *testing.T) {
	fixture := newBackupFixture(t)
	root := fixture.service.root
	orphan := filepath.Join(root, "beat-orphan.zip")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan archive: %v", err)
	}
	partial := filepath.Join(root, "beat-partial.zip.partial")
	if err := os.WriteFile(partial, []byte("partial"), 0o600); err != nil {
		t.Fatalf("write partial archive: %v", err)
	}
	notes := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(notes, []byte("notes"), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	directory := filepath.Join(root, "beat-folder.zip")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	staleJournal := filepath.Join(fixture.dataDir, ".restore-journal-stale")
	if err := os.WriteFile(staleJournal, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write stale journal: %v", err)
	}
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Quarantined != 1 || outcome.PartialRemoved != 2 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan archive was not moved: %v", err)
	}
	quarantined := filepath.Join(root, "quarantine", "beat-orphan.zip")
	if info, err := os.Lstat(quarantined); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("quarantined archive = %#v, %v", info, err)
	}
	for _, path := range []string{notes, directory} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("unrelated entry %s was touched: %v", path, err)
		}
	}
	if _, err := os.Lstat(staleJournal); !os.IsNotExist(err) {
		t.Fatalf("stale journal survived: %v", err)
	}
	count, err := fixture.service.records.CountQuarantine(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("quarantine inventory = %d, %v", count, err)
	}
	// 第二次对账必须稳定无改动。
	again, err := fixture.service.Reconcile(t.Context())
	if err != nil || again.Changed() {
		t.Fatalf("second reconciliation = %#v, %v", again, err)
	}
}

func TestReconcileQuarantineAvoidsOverwritingExistingArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	orphan := filepath.Join(fixture.service.root, "beat-orphan.zip")
	for round := 0; round < 2; round++ {
		if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
			t.Fatalf("write orphan archive: %v", err)
		}
		if _, err := fixture.service.Reconcile(t.Context()); err != nil {
			t.Fatalf("reconcile round %d: %v", round, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(fixture.service.root, "quarantine"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("quarantine entries = %d, %v", len(entries), err)
	}
	count, err := fixture.service.records.CountQuarantine(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("quarantine inventory = %d, %v", count, err)
	}
}

func TestReconcileExpiresTerminalBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	records := fixture.service.records
	old := model.BackupRecord{ID: "expired", Filename: "beat-expired.zip", Source: "generated",
		State: model.BackupStateFailed, CreatedAt: time.Now().UTC().Add(-120 * 24 * time.Hour)}
	if err := records.Create(t.Context(), &old); err != nil {
		t.Fatalf("create expired record: %v", err)
	}
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.Expired != 1 {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	if loadBackupRow(t, fixture, "expired") != nil {
		t.Fatal("expired terminal record survived reconciliation")
	}
	if !hasAuditAction(t, fixture, "backup.reconcile.expired") {
		t.Fatal("expiry was not audited")
	}
}
