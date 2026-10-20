package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineStageAndApplyEmptyInstance(t *testing.T) {
	fixture := newBackupFixture(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	destination := t.TempDir()
	if err := StageOffline(t.Context(), destination, archive); err != nil {
		t.Fatal(err)
	}
	pending, err := PendingRestore(destination)
	if err != nil || !pending {
		t.Fatal("journal not pending")
	}
	if err := StageOffline(t.Context(), destination, bytes.NewReader(nil)); err == nil {
		t.Fatal("duplicate pending accepted")
	}
	if err := ApplyPending(t.Context(), destination, filepath.Join(destination, "beat.db"), filepath.Join(destination, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	pending, err = PendingRestore(destination)
	if err != nil || pending {
		t.Fatal("restore not applied")
	}
}

func TestOfflineStageFailuresLeaveNoPendingJournal(t *testing.T) {
	destination := t.TempDir()
	if err := StageOffline(t.Context(), destination, bytes.NewBufferString("invalid")); err == nil {
		t.Fatal("invalid archive accepted")
	}
	if _, err := os.Stat(filepath.Join(destination, "restore.pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid journal published")
	}
	if err := writeOfflineArchive(filepath.Join(destination, "missing", "archive"), bytes.NewReader(nil)); err == nil {
		t.Fatal("missing parent ignored")
	}
	if err := publishOfflineArchive(destination, filepath.Join(destination, "absent")); err == nil {
		t.Fatal("missing archive ignored")
	}
	if err := os.WriteFile(filepath.Join(destination, "restore.pending.json"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StageOffline(t.Context(), destination, bytes.NewReader(nil)); err == nil {
		t.Fatal("corrupt journal accepted")
	}
}

func TestOfflineStageIOFailures(t *testing.T) {
	if err := StageOffline(t.Context(), filepath.Join(t.TempDir(), "absent", "data"), bytes.NewReader(nil)); err == nil {
		t.Fatal("absent staging parent accepted")
	}
	directory := t.TempDir()
	if err := StageOffline(t.Context(), directory, offlineFailedReader{}); err == nil {
		t.Fatal("input read error ignored")
	}
	if err := os.WriteFile(filepath.Join(directory, "backups"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishOfflineArchive(directory, "absent"); err == nil {
		t.Fatal("backup directory obstruction ignored")
	}
	directory = t.TempDir()
	archive := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "restore.pending.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publishOfflineArchive(directory, archive); err == nil {
		t.Fatal("journal publish error ignored")
	}
	entries, err := os.ReadDir(filepath.Join(directory, "backups"))
	if err != nil || len(entries) != 0 {
		t.Fatal("orphan archive remained")
	}
}

type offlineFailedReader struct{}

func (offlineFailedReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }
