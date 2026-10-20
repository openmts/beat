package applicationsecrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateFilesAndLeasesRejectBrokenPaths(t *testing.T) {
	directory := privateTestDirectory(t)
	filePath := filepath.Join(directory, "file")
	writeRootFault(t, filePath, []byte("regular"))
	for _, path := range []string{filepath.Join(directory, "missing", "data"), filePath, filepath.Join(filePath, "child")} {
		if lease, err := AcquireLease(path); err == nil {
			_ = lease.Close()
			t.Fatal("unsafe lifecycle path accepted")
		}
	}
	if _, err := canonicalDataPath(string([]byte{'x', 0})); err == nil {
		t.Fatal("NUL path accepted")
	}
	if _, err := privateDirectoryOwner(filePath); err == nil {
		t.Fatal("file accepted as directory")
	}
	if err := publishNewPrivateFile(filepath.Join(filePath, "key"), nil); err == nil {
		t.Fatal("file parent accepted")
	}
	if err := syncDirectory(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("missing directory synchronized")
	}
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := samePrivateFile(filepath.Join(directory, "missing"), file); err == nil {
		t.Fatal("unlinked descriptor accepted")
	}
	other := filepath.Join(directory, "other")
	writeRootFault(t, other, nil)
	if err := samePrivateFile(other, file); err == nil {
		t.Fatal("replacement file accepted")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := samePrivateFile(filePath, file); err == nil {
		t.Fatal("closed descriptor accepted")
	}
	if err := writeSyncedFile(file, nil); err == nil {
		t.Fatal("closed file write accepted")
	}
}

func TestRootRotationRejectsOutsideOrMissingDatabase(t *testing.T) {
	directory := privateTestDirectory(t)
	if err := createRotationDirectory(directory); err == nil {
		t.Fatal("existing rotation path replaced")
	}
	if _, err := openRootRotationDB(RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(t.TempDir(), "outside.db")}); err == nil {
		t.Fatal("outside database accepted")
	}
	if _, err := openRootRotationDB(RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "missing.db")}); err == nil {
		t.Fatal("missing database created by rotation")
	}
}

func TestRootRecoveryArtifactsAreStrictAndBounded(t *testing.T) {
	directory := privateTestDirectory(t)
	if err := removeRotationDirectory(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("missing artifact ignored")
	}
	if err := removeRotationFile(directory, "unknown"); err == nil {
		t.Fatal("unregistered artifact accepted")
	}
	writeRootFault(t, filepath.Join(directory, "old.key"), []byte("old"))
	if err := os.Chmod(filepath.Join(directory, "old.key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeRotationFile(directory, "old.key"); err == nil {
		t.Fatal("unsafe artifact deleted")
	}
	if err := publishJournalRoot(directory, "next.key"); err == nil {
		t.Fatal("missing journal key published")
	}
	if err := finishRootJournal(directory, nil); err == nil {
		t.Fatal("missing journal finished")
	}
	for _, body := range [][]byte{[]byte("{}{}"), bytes.Repeat([]byte{'x'}, 4097), []byte(`{"version":1}`)} {
		writeRootFault(t, filepath.Join(directory, "journal.json"), body)
		if _, err := readRootJournal(directory); err == nil {
			t.Fatal("invalid journal accepted")
		}
	}
}
