package applicationsecrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/secretbox"
)

// RecoverRootFiles 在 Lease 内、恢复备份和常规 SQLite 迁移之前调用。
func RecoverRootFiles(ctx context.Context, dataDir, databasePath string) (resultErr error) {
	marker, err := rootRotationMarker(dataDir)
	if err != nil || marker == "" {
		return err
	}
	database, err := openRootRotationDB(RootRotationConfig{DataDir: dataDir, DatabasePath: databasePath})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	_, err = recoverRootJournal(ctx, database, dataDir)
	return err
}

func rootRotationMarker(directory string) (string, error) {
	found := ""
	for _, name := range rotationDirectories {
		_, err := os.Lstat(filepath.Join(directory, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if found != "" {
			return "", errors.New("ambiguous root rotation state")
		}
		found = name
	}
	return found, nil
}

func recoverRootJournal(ctx context.Context, database *sql.DB, directory string) (bool, error) {
	marker, err := rootRotationMarker(directory)
	if err != nil || marker == "" {
		return false, err
	}
	if marker != rotationJournalDir {
		root, err := snapshotRoot(filepath.Join(directory, "admin-data.key"))
		if err != nil {
			return true, err
		}
		if _, err := validateWithRoot(ctx, database, root); err != nil {
			return true, err
		}
		return true, removeRotationDirectory(filepath.Join(directory, marker))
	}
	if err := recoverPublishedJournal(ctx, database, directory); err != nil {
		return true, err
	}
	return true, finishRootJournal(directory, nil)
}

func recoverPublishedJournal(ctx context.Context, database *sql.DB, directory string) error {
	journal, err := readRootJournal(filepath.Join(directory, rotationJournalDir))
	if err != nil {
		return err
	}
	state, err := readState(ctx, database)
	if err != nil {
		return err
	}
	name, fingerprint := "", ""
	if state.RootGeneration == journal.PreviousGeneration && state.RootFingerprint == journal.PreviousFingerprint {
		name, fingerprint = "old.key", journal.PreviousFingerprint
	} else if state.RootGeneration == journal.NextGeneration && state.RootFingerprint == journal.NextFingerprint {
		name, fingerprint = "next.key", journal.NextFingerprint
	} else {
		return errors.New("root rotation generation does not match the database")
	}
	root, err := snapshotRoot(filepath.Join(directory, rotationJournalDir, name))
	if err != nil {
		return err
	}
	if root.RootFingerprint() != fingerprint {
		return ErrUnhealthy
	}
	if _, err := validateWithRoot(ctx, database, root); err != nil {
		return err
	}
	if err := verifyCurrentRoot(directory, journal); err != nil {
		return err
	}
	if err := checkpointSQLite(ctx, database); err != nil {
		return err
	}
	return publishJournalRoot(directory, name)
}

func verifyCurrentRoot(directory string, journal rootJournal) error {
	root, err := snapshotRoot(filepath.Join(directory, "admin-data.key"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if root.RootFingerprint() != journal.PreviousFingerprint && root.RootFingerprint() != journal.NextFingerprint {
		return ErrUnhealthy
	}
	return nil
}

func validateWithRoot(ctx context.Context, database databaseReader, root *secretbox.Manager) (lifecycleState, error) {
	state, err := readState(ctx, database)
	if err != nil {
		return state, err
	}
	if state.Phase != "ready" || state.CursorID != "" || state.CursorSource != 0 {
		return state, ErrUnhealthy
	}
	if err := validateNotificationState(ctx, database); err != nil {
		return state, err
	}
	ring, err := loadKeyring(ctx, database, keyringConfig{root: root, state: state})
	if err != nil {
		return state, err
	}
	_, err = validateResources(ctx, database, validationConfig{ring: ring, requireActive: true})
	return state, err
}

func rejectPendingRestore(directory string) error {
	value, err := readPrivateDocument(filepath.Join(directory, "restore.pending.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(value, &journal); err != nil {
		return errors.New("invalid restore state")
	}
	if journal.State != "applied" {
		return errors.New("root rotation cannot overlap pending restore")
	}
	return nil
}
