package applicationsecrets

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type rootJournal struct {
	Version             int    `json:"version"`
	PreviousFingerprint string `json:"previous_fingerprint"`
	NextFingerprint     string `json:"next_fingerprint"`
	PreviousGeneration  int64  `json:"previous_generation"`
	NextGeneration      int64  `json:"next_generation"`
}

func (rotator *rootRotator) publishJournal(prepare string) error {
	data, err := json.Marshal(rotator.journal)
	if err != nil {
		return err
	}
	if err := publishNewPrivateFile(filepath.Join(prepare, "journal.json"), data); err != nil {
		return err
	}
	if err := syncDirectory(prepare); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_journal_saved"); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, prepare, unix.AT_FDCWD,
		filepath.Join(rotator.config.DataDir, rotationJournalDir), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := syncDirectory(rotator.config.DataDir); err != nil {
		return err
	}
	return rotator.checkpoint("root_journal_published")
}

func createRotationDirectory(path string) error {
	owner, err := privateDirectoryOwner(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, int(owner), -1); err != nil {
			return err
		}
	}
	return syncDirectory(filepath.Dir(path))
}

func readRootJournal(directory string) (rootJournal, error) {
	var journal rootJournal
	value, err := readPrivateDocument(filepath.Join(directory, "journal.json"))
	if err != nil {
		return journal, err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return journal, errors.New("invalid root rotation journal")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return journal, errors.New("invalid root rotation journal")
	}
	if journal.Version != 1 || journal.PreviousGeneration < 1 || journal.NextGeneration != journal.PreviousGeneration+1 ||
		!validFingerprint(journal.PreviousFingerprint) || !validFingerprint(journal.NextFingerprint) || journal.PreviousFingerprint == journal.NextFingerprint {
		return journal, errors.New("invalid root rotation journal")
	}
	return journal, nil
}

func validFingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == rootKeyBytes && hex.EncodeToString(decoded) == value
}

func readPrivateDocument(path string) (value []byte, resultErr error) {
	owner, err := privateDirectoryOwner(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path, unix.O_RDONLY, owner)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	const maximumJournalBytes = 4096
	value, err = io.ReadAll(io.LimitReader(file, maximumJournalBytes+1))
	if err != nil || len(value) > maximumJournalBytes {
		return nil, errors.New("invalid lifecycle document")
	}
	return value, nil
}

func publishJournalRoot(directory, name string) error {
	key, err := readRootKey(filepath.Join(directory, rotationJournalDir, name))
	if err != nil {
		return err
	}
	defer clear(key)
	owner, err := privateDirectoryOwner(directory)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".beat-root-publish-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if os.Geteuid() == 0 {
		if err := file.Chown(int(owner), -1); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := writeSyncedFile(file, key); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(directory, "admin-data.key")); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func finishRootJournal(directory string, checkpoint func(string) error) error {
	completed := filepath.Join(directory, rotationCompleteDir)
	if err := unix.Renameat2(unix.AT_FDCWD, filepath.Join(directory, rotationJournalDir), unix.AT_FDCWD, completed, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if checkpoint != nil {
		if err := checkpoint("root_cleanup_published"); err != nil {
			return err
		}
	}
	return removeRotationDirectory(completed)
}

func removeRotationDirectory(path string) error {
	if _, err := privateDirectoryOwner(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeRotationFile(path, entry.Name()); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func removeRotationFile(directory, name string) error {
	if name != "old.key" && name != "next.key" && name != "journal.json" && !strings.HasPrefix(name, ".beat-secret-") {
		return errors.New("unexpected root rotation artifact")
	}
	owner, err := privateDirectoryOwner(directory)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, name)
	file, err := openPrivateFile(path, unix.O_RDONLY, owner)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}
