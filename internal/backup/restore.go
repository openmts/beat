package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

type renameOperation func(string, string) error

type Applier struct {
	rename renameOperation
	now    func() time.Time
}

func NewApplier() *Applier {
	return &Applier{rename: os.Rename, now: func() time.Time { return time.Now().UTC() }}
}

func ApplyPending(ctx context.Context, dataDir, dbPath, mtsPath string) error {
	return NewApplier().ApplyPending(ctx, dataDir, dbPath, mtsPath)
}

func PendingRestore(dataDir string) (bool, error) {
	journal, err := readJournal(filepath.Join(dataDir, "restore.pending.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pending restore state: %w", err)
	}
	return journal.State == "pending", nil
}

func (applier *Applier) ApplyPending(
	ctx context.Context,
	dataDir string,
	dbPath string,
	mtsPath string,
) error {
	journalPath := filepath.Join(dataDir, "restore.pending.json")
	journal, err := readJournal(journalPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending restore: %w", err)
	}
	if journal.State != "pending" {
		return nil
	}
	if err := validateJournalArchive(dataDir, journal.ArchivePath); err != nil {
		return err
	}
	validated, err := validateArchive(ctx, journal.ArchivePath, dataDir)
	if err != nil {
		return fmt.Errorf("revalidate pending restore: %w", err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	token := strings.ReplaceAll(journalTimestamp(applier.now()), ":", "")
	prepared, err := prepareRestore(validated, dbPath, mtsPath, dataDir, token)
	if err != nil {
		return err
	}
	defer prepared.cleanup()
	rollbacks, err := applier.activate(ctx, prepared, token)
	if err != nil {
		return err
	}
	if err := validateApplied(ctx, dbPath, mtsPath); err != nil {
		return errors.Join(err, applier.rollbackApplied(prepared, rollbacks))
	}
	if err := applier.completeJournal(journalPath, &journal, rollbacks, dbPath, mtsPath); err != nil {
		return errors.Join(err, applier.rollbackApplied(prepared, rollbacks))
	}
	prepared.committed = true
	return nil
}

func (applier *Applier) completeJournal(
	path string,
	journal *model.RestoreJournal,
	rollbacks map[string]string,
	dbPath string,
	mtsPath string,
) error {
	appliedAt := applier.now()
	journal.State = "applied"
	journal.AppliedAt = &appliedAt
	journal.SQLiteRollback = rollbacks[dbPath]
	journal.MTSRollback = rollbacks[mtsPath]
	return writeJournal(path, *journal)
}

func validateJournalArchive(dataDir, archivePath string) error {
	backupRoot, err := filepath.Abs(filepath.Join(dataDir, "backups"))
	if err != nil {
		return err
	}
	archive, err := filepath.Abs(archivePath)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(backupRoot, archive)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("pending restore archive is outside the backup directory")
	}
	return nil
}
