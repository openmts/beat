package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingRestoreStatus(t *testing.T) {
	dataDir := t.TempDir()
	pending, err := PendingRestore(dataDir)
	if err != nil || pending {
		t.Fatalf("missing journal = pending:%v error:%v", pending, err)
	}
	journal := restoreJournal("backup", filepath.Join(dataDir, "backups", "backup.zip"))
	if err := writeJournal(filepath.Join(dataDir, "restore.pending.json"), journal); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	pending, err = PendingRestore(dataDir)
	if err != nil || !pending {
		t.Fatalf("pending journal = pending:%v error:%v", pending, err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "restore.pending.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid journal: %v", err)
	}
	if _, err := PendingRestore(dataDir); err == nil {
		t.Fatal("invalid restore journal reported healthy")
	}
}

func TestBackupRunningStatus(t *testing.T) {
	fixture := newBackupFixture(t)
	if fixture.service.Running() {
		t.Fatal("idle backup service reported running")
	}
	fixture.service.mu.Lock()
	fixture.service.running = true
	fixture.service.mu.Unlock()
	if !fixture.service.Running() {
		t.Fatal("active backup service reported idle")
	}
}
