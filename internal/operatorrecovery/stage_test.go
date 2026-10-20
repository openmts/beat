package operatorrecovery

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/store"
)

func recoveryArchive(t *testing.T) string {
	t.Helper()
	config := fixture(t)
	database, err := store.NewSQLiteStore(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	metrics, err := store.NewMTSStore(filepath.Join(config.DataDir, "beat_mts"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = metrics.Close() }()
	secrets, err := applicationsecrets.Open(t.Context(), applicationsecrets.Config{Database: database.DB, DataDir: config.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer secrets.Close()
	service, err := backup.NewService(t.Context(), store.NewBackupStore(database.DB), database, metrics, filepath.Join(config.DataDir, "backups"), filepath.Join(config.DataDir, "admin-data.key"), "test")
	if err != nil {
		t.Fatal(err)
	}
	service.SetSecretLifecycle(secrets)
	record, err := service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	service.Wait()
	file, _, err := service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func TestFirstRunStageInspectAndStartupApply(t *testing.T) {
	archive := recoveryArchive(t)
	directory := filepath.Join(privateTemp(t), "empty")
	var output bytes.Buffer
	if err := Run([]string{"stage-restore", "--data-dir", directory, "--archive", archive}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "restore_staged") {
		t.Fatal("stage success not reported")
	}
	if _, err := os.Lstat(filepath.Join(directory, "beat.db")); !os.IsNotExist(err) {
		t.Fatal("stage created live database")
	}
	session, err := Open(t.Context(), Config{DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	if session.Status.State != "restore_staged" || !session.Status.PendingRestore {
		t.Fatal("pending empty instance unavailable")
	}
	if err := session.StageRestore(t.Context(), archive); err == nil {
		t.Fatal("restaged pending")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backup.ApplyPending(t.Context(), directory, filepath.Join(directory, "beat.db"), filepath.Join(directory, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	database, err := openDatabase(filepath.Join(directory, "beat.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	count, err := store.NewAdminStore(database).CountUsers(t.Context())
	if err != nil || count != 1 {
		t.Fatal("restored owner absent; bootstrap would not be suppressed")
	}
}

func TestStageFailuresAndPrivateStateChanges(t *testing.T) {
	config := fixture(t)
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	archive := filepath.Join(privateTemp(t), "bad.zip")
	if err := os.WriteFile(archive, []byte("bad archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StageRestore(t.Context(), archive); err == nil {
		t.Fatal("bad archive accepted")
	}
	if err := os.Chmod(config.DatabasePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := session.StageRestore(t.Context(), archive); err == nil {
		t.Fatal("public database accepted")
	}
	input := UnlockInput{Username: "owner", Password: "valid replacement", Repeat: "valid replacement", Confirmation: session.Confirmation("owner")}
	if err := session.Unlock(t.Context(), input); err == nil {
		t.Fatal("changed permissions accepted")
	}
	empty, err := Open(t.Context(), Config{DataDir: privateTemp(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := finishCommand(empty, commandOptions{operation: "unknown"}, io.Discard); err == nil {
		t.Fatal("unknown operation accepted")
	}
	entries, err := os.ReadDir(config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if stagedEntries(entries) {
		t.Fatal("unknown staged files accepted")
	}
}

func TestInspectionPendingAndMalformedDatabase(t *testing.T) {
	config := Config{DataDir: privateTemp(t)}
	config.DatabasePath = filepath.Join(config.DataDir, "beat.db")
	journal := filepath.Join(config.DataDir, "restore.pending.json")
	if err := os.WriteFile(journal, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("invalid pending accepted")
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.DatabasePath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("corrupt SQLite accepted")
	}
}
