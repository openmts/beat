package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func restoreJournalService(t *testing.T) (*Service, string) {
	t.Helper()
	directory := t.TempDir()
	service := &Service{root: filepath.Join(directory, "backups"), now: time.Now}
	return service, filepath.Join(directory, "restore.pending.json")
}

func writeRestoreJournal(t *testing.T, path, backupID, state string) {
	t.Helper()
	content := `{"backup_id":"` + backupID + `","archive_path":"/var/backups/` + backupID +
		`.zip","state":"` + state + `","created_at":"2026-10-15T09:00:00Z"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreJournalCleanupFaults 覆盖恢复 journal 清理的缺失、他人待恢复与不可解析分支。
func TestRestoreJournalCleanupFaults(t *testing.T) {
	t.Run("journal absent", func(t *testing.T) {
		service, _ := restoreJournalService(t)
		if err := service.clearJournal("backup-a"); err != nil {
			t.Fatalf("missing journal cleanup error = %v", err)
		}
	})
	t.Run("journal belongs to another backup", func(t *testing.T) {
		service, path := restoreJournalService(t)
		writeRestoreJournal(t, path, "backup-b", "pending")
		if err := service.clearJournal("backup-a"); err != nil {
			t.Fatalf("foreign journal cleanup error = %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("foreign journal was removed: %v", err)
		}
	})
	t.Run("journal unreadable", func(t *testing.T) {
		service, path := restoreJournalService(t)
		if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := service.clearJournal("backup-a"); err == nil {
			t.Fatal("malformed journal was cleared")
		}
	})
	t.Run("own journal removed", func(t *testing.T) {
		service, path := restoreJournalService(t)
		writeRestoreJournal(t, path, "backup-a", "pending")
		if err := service.clearJournal("backup-a"); err != nil {
			t.Fatalf("own journal cleanup error = %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("own journal still present: %v", err)
		}
	})
}

// TestRestoreJournalConflictFaults 覆盖暂存第二个恢复前的 journal 冲突判定。
func TestRestoreJournalConflictFaults(t *testing.T) {
	t.Run("journal unreadable", func(t *testing.T) {
		service, path := restoreJournalService(t)
		if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := service.rejectConflictingJournal("backup-a"); err == nil {
			t.Fatal("malformed pending journal was accepted")
		}
	})
	t.Run("pending journal for another backup", func(t *testing.T) {
		service, path := restoreJournalService(t)
		writeRestoreJournal(t, path, "backup-b", "pending")
		if err := service.rejectConflictingJournal("backup-a"); !errors.Is(err, ErrRestoreStaged) {
			t.Fatalf("conflicting journal error = %v", err)
		}
	})
	t.Run("pending journal for this backup", func(t *testing.T) {
		service, path := restoreJournalService(t)
		writeRestoreJournal(t, path, "backup-a", "pending")
		if err := service.rejectConflictingJournal("backup-a"); err != nil {
			t.Fatalf("resumed journal error = %v", err)
		}
	})
	t.Run("applied journal", func(t *testing.T) {
		service, path := restoreJournalService(t)
		writeRestoreJournal(t, path, "backup-b", "applied")
		if err := service.rejectConflictingJournal("backup-a"); err != nil {
			t.Fatalf("applied journal error = %v", err)
		}
	})
}

// TestAbortStagingMissingRecord 覆盖暂存回滚在记录缺失时的空操作。
func TestAbortStagingMissingRecord(t *testing.T) {
	service, _ := restoreJournalService(t)
	service.records = &fakeRecordStore{}
	if err := service.abortStaging(t.Context(), "absent", SystemActor()); err != nil {
		t.Fatalf("abort staging without a record = %v", err)
	}
	staged := &model.BackupRecord{ID: "backup-a", State: model.BackupStateReady}
	service.records = &fakeRecordStore{record: staged}
	if err := service.abortStaging(t.Context(), "backup-a", SystemActor()); err != nil {
		t.Fatalf("abort staging for a ready record = %v", err)
	}
}
