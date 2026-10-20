package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
)

const maximumReceipts = 100000

func (store *journalStore) prepareReceipts() error {
	directory := filepath.Join(store.root, ".acknowledged")
	if err := createPrivateDirectory(directory); err != nil {
		return err
	}
	if err := privateDirectory(directory, true); err != nil {
		return ErrStorage
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) >= maximumReceipts {
		return ErrStorage
	}
	store.receiptCount = len(entries)
	return syncDirectory(store.root)
}

func (store *journalStore) acknowledged(id string) (bool, error) {
	path := filepath.Join(store.root, ".acknowledged", journalName(id))
	if err := privateFile(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, ErrStorage
	}
	return true, nil
}

func (store *journalStore) markAcknowledged(id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	directory := filepath.Join(store.root, ".acknowledged")
	path := filepath.Join(directory, journalName(id))
	if exists, err := store.acknowledged(id); err != nil {
		return err
	} else if exists {
		return syncDirectory(directory)
	}
	if store.receiptCount >= maximumReceipts {
		return ErrStorage
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return privateFile(path)
	}
	if err != nil {
		return ErrStorage
	}
	if err := secureCreatedFile(file); err != nil {
		_ = file.Close()
		return ErrStorage
	}
	_, writeErr := file.Write([]byte("acknowledged\n"))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return ErrStorage
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	store.receiptCount++
	return nil
}
