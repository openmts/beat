package agentoperations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type journal struct {
	Transfer *transferProgress `json:"transfer,omitempty"`
	Version  int               `json:"version"`
	State    string            `json:"state"`
	Lease    protocol.Lease    `json:"lease"`
	Result   *protocol.Result  `json:"result,omitempty"`
}

type journalStore struct {
	root         string
	unlock       func() error
	mu           sync.Mutex
	receiptCount int
}

func openJournals(path string) (*journalStore, error) {
	if err := createPrivateDirectory(path); err != nil {
		return nil, err
	}
	if err := privateDirectory(path, true); err != nil {
		return nil, ErrStorage
	}
	unlock, err := lockState(filepath.Join(path, ".lock"))
	if err != nil {
		return nil, ErrStorage
	}
	store := &journalStore{root: path, unlock: unlock}
	if err := store.prepareReceipts(); err != nil {
		_ = unlock()
		return nil, err
	}
	return store, nil
}

func journalName(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}

func (store *journalStore) directory(id string) string {
	return filepath.Join(store.root, journalName(id))
}

func (store *journalStore) load(id string) (journal, error) {
	path := filepath.Join(store.directory(id), "journal.json")
	if err := privateFile(path); err != nil {
		return journal{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return journal{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, maximumJournalBytes+1))
	decoder.DisallowUnknownFields()
	var result journal
	if err := decoder.Decode(&result); err != nil {
		return journal{}, ErrStorage
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return journal{}, ErrStorage
	}
	if err := validateJournal(result); err != nil {
		return journal{}, err
	}
	if result.Lease.AttemptID != id {
		return journal{}, ErrStorage
	}
	return result, nil
}

func validateJournal(record journal) error {
	if err := validateTransferProgress(record.Transfer); err != nil {
		return err
	}
	if record.Version != 1 || !validAttemptID(record.Lease.AttemptID) || len(record.Lease.Nonce) != 64 {
		return ErrStorage
	}
	if _, err := hex.DecodeString(record.Lease.Nonce); err != nil {
		return ErrStorage
	}
	return validateJournalState(record)
}

func validateJournalState(record journal) error {
	switch record.State {
	case "leased":
		return validateLeasedJournal(record)
	case "authorized", "running":
		if record.Result != nil {
			return ErrStorage
		}
	case "completed":
		if record.Result == nil {
			return ErrStorage
		}
		return record.Result.Validate(protocol.MaximumOutputBytes)
	default:
		return ErrStorage
	}
	return nil
}

func validateLeasedJournal(record journal) error {
	if record.Result != nil {
		return ErrStorage
	}
	if isFileLease(record.Lease) {
		if record.Lease.Payload.File == nil {
			return ErrStorage
		}
		return protocol.ValidateFile(*record.Lease.Payload.File)
	}
	return protocol.ValidateCommand(record.Lease.Payload.Command)
}

func (store *journalStore) save(record journal) (resultErr error) {
	if err := validateJournal(record); err != nil {
		return err
	}
	content, err := json.Marshal(record)
	if err != nil || len(content) > maximumJournalBytes {
		return ErrStorage
	}
	directory := store.directory(record.Lease.AttemptID)
	if err := createPrivateDirectory(directory); err != nil {
		return err
	}
	if err := privateDirectory(directory, true); err != nil {
		return ErrStorage
	}
	if err := atomicPrivateWrite(filepath.Join(directory, "journal.json"), content); err != nil {
		return err
	}
	return syncDirectory(store.root)
}

func atomicPrivateWrite(path string, content []byte) (resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".journal-")
	if err != nil {
		return ErrStorage
	}
	temporary := file.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary)
		}
	}()
	if err := secureCreatedFile(file); err != nil {
		_ = file.Close()
		return ErrStorage
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return ErrStorage
	}
	if err := replacePrivate(temporary, path); err != nil {
		return ErrStorage
	}
	return syncDirectory(filepath.Dir(path))
}

func (store *journalStore) remove(id string) error {
	directory := store.directory(id)
	if err := privateDirectory(directory, true); err != nil {
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return ErrStorage
	}
	return syncDirectory(store.root)
}

func (store *journalStore) list() ([]journal, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil || len(entries) > maximumJournals+2 {
		return nil, ErrStorage
	}
	records := []journal{}
	for _, entry := range entries {
		if entry.Name() == ".lock" || entry.Name() == ".acknowledged" {
			continue
		}
		record, err := store.loadDirectory(entry.Name())
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (store *journalStore) loadDirectory(name string) (journal, error) {
	if len(name) != 64 || strings.ToLower(name) != name {
		return journal{}, ErrStorage
	}
	if _, err := hex.DecodeString(name); err != nil {
		return journal{}, ErrStorage
	}
	path := filepath.Join(store.root, name, "journal.json")
	if err := privateFile(path); err != nil {
		return journal{}, ErrStorage
	}
	content, err := readBoundedPrivate(path, maximumJournalBytes)
	if err != nil {
		return journal{}, err
	}
	var record journal
	if err := json.Unmarshal(content, &record); err != nil || journalName(record.Lease.AttemptID) != name {
		return journal{}, ErrStorage
	}
	return store.load(record.Lease.AttemptID)
}

func readBoundedPrivate(path string, maximum int) (content []byte, resultErr error) {
	if err := privateFile(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrStorage
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	content, err = io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(content) > maximum {
		return nil, ErrStorage
	}
	return content, nil
}
