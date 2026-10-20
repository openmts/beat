package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type pendingRestoreFixture struct {
	dataDir, dbPath, mtsPath, backupID, generation string
}

// preparePendingRestore 构造一个“归档已在 backups 目录、journal 已写”的待恢复数据目录。
func preparePendingRestore(t *testing.T) pendingRestoreFixture {
	t.Helper()
	fixture := newBackupFixture(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	uploaded, validateErr := fixture.service.ValidateUpload(t.Context(), archive)
	closeErr := archive.Close()
	if validateErr != nil || closeErr != nil {
		t.Fatalf("validate upload: %v, close: %v", validateErr, closeErr)
	}
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "backups")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create target backup directory: %v", err)
	}
	destination := filepath.Join(root, uploaded.Filename)
	if err := copyFile(filepath.Join(fixture.service.root, uploaded.Filename), destination, 0o600); err != nil {
		t.Fatalf("copy target archive: %v", err)
	}
	journal := model.RestoreJournal{BackupID: uploaded.ID, ArchivePath: destination,
		State: "pending", Generation: "gen-1", CreatedAt: time.Now().UTC()}
	if err := writeJournal(filepath.Join(dataDir, "restore.pending.json"), journal); err != nil {
		t.Fatalf("write restore journal: %v", err)
	}
	return pendingRestoreFixture{dataDir: dataDir, dbPath: filepath.Join(dataDir, "beat.db"),
		mtsPath: filepath.Join(dataDir, "beat_mts"), backupID: uploaded.ID, generation: "gen-1"}
}

func seedLiveBackupRecord(t *testing.T, dbPath, id, state, generation string) {
	t.Helper()
	database, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open live database: %v", err)
	}
	defer func() { _ = database.Close() }()
	record := model.BackupRecord{ID: id, Filename: "live.zip", Source: "uploaded", State: state,
		CreatedAt: time.Now().UTC(), RestoreGeneration: generation,
		PreviousState: model.BackupStateValidated}
	if err := store.NewBackupStore(database.DB).Create(t.Context(), &record); err != nil {
		if _, updateErr := database.DB.Exec(`UPDATE admin_backups SET state = ?,
			restore_generation = ?, previous_state = ? WHERE id = ?`,
			state, generation, model.BackupStateValidated, id); updateErr != nil {
			t.Fatalf("seed live record: %v, %v", err, updateErr)
		}
	}
}

// TestApplyPendingRequiresStagedRecordAndGeneration 证明未确认的 journal 绝不替换活动库。
func TestApplyPendingRequiresStagedRecordAndGeneration(t *testing.T) {
	pending := preparePendingRestore(t)
	seedLiveBackupRecord(t, pending.dbPath, pending.backupID, model.BackupStateReady, pending.generation)
	if err := ApplyPending(t.Context(), pending.dataDir, pending.dbPath, pending.mtsPath); err != nil {
		t.Fatalf("apply pending: %v", err)
	}
	if stillPending, err := PendingRestore(pending.dataDir); err != nil || !stillPending {
		t.Fatalf("ready record authorized a restore: %v, %v", stillPending, err)
	}
	if claimableJournal(t, pending.dataDir).State != "pending" {
		t.Fatal("unconfirmed journal was rewritten")
	}
	// 代次不符同样不得应用。
	seedLiveBackupRecord(t, pending.dbPath, pending.backupID, model.BackupStateStaged, "other")
	if err := ApplyPending(t.Context(), pending.dataDir, pending.dbPath, pending.mtsPath); err != nil {
		t.Fatalf("apply pending with mismatched generation: %v", err)
	}
	if stillPending, err := PendingRestore(pending.dataDir); err != nil || !stillPending {
		t.Fatalf("mismatched generation authorized a restore: %v, %v", stillPending, err)
	}
	// staged + 同代次才允许替换活动库。
	seedLiveBackupRecord(t, pending.dbPath, pending.backupID, model.BackupStateStaged, pending.generation)
	if err := ApplyPending(t.Context(), pending.dataDir, pending.dbPath, pending.mtsPath); err != nil {
		t.Fatalf("apply confirmed restore: %v", err)
	}
	if stillPending, err := PendingRestore(pending.dataDir); err != nil || stillPending {
		t.Fatalf("confirmed restore was not applied: %v, %v", stillPending, err)
	}
}

func TestRestoreAuthorizedRejectsUnknownJournals(t *testing.T) {
	pending := preparePendingRestore(t)
	if authorized, err := restoreAuthorized(t.Context(), filepath.Join(t.TempDir(), "absent.db"),
		model.RestoreJournal{BackupID: pending.backupID, State: "pending",
			Generation: pending.generation}); err != nil || authorized {
		t.Fatalf("absent live database authorized restore: %v, %v", authorized, err)
	}
	offline := model.RestoreJournal{BackupID: pending.backupID, State: "pending",
		Origin: model.RestoreOriginOffline}
	if authorized, err := restoreAuthorized(t.Context(), filepath.Join(t.TempDir(), "absent.db"), offline); err != nil || !authorized {
		t.Fatalf("offline journal must stay authorized: %v, %v", authorized, err)
	}
	if authorized, err := restoreAuthorized(t.Context(), pending.dbPath,
		model.RestoreJournal{BackupID: pending.backupID, State: "pending"}); err != nil || authorized {
		t.Fatalf("journal without generation authorized restore: %v, %v", authorized, err)
	}
}

func claimableJournal(t *testing.T, dataDir string) model.RestoreJournal {
	t.Helper()
	journal, err := readJournal(filepath.Join(dataDir, "restore.pending.json"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return journal
}
