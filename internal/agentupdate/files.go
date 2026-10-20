package agentupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

const stateDirectory = ".beat-agent-update"

const journalFile = "journal.json"

const downloadFile = "download.part"

const previousFile = "previous.bin"

const rollbackFile = "rollback.part"

type directory struct {
	file   *os.File
	device uint64
}

type installation struct {
	root, state *directory
	name        string
	lock        *os.File
}

func openInstallation(executable string, create bool) (*installation, error) {
	root, err := openDirectory(filepath.Dir(executable))
	if err != nil {
		return nil, err
	}
	result := &installation{root: root, name: filepath.Base(executable)}
	state, err := openChildDirectory(root, stateDirectory, create)
	if err != nil {
		_ = root.file.Close()
		return nil, err
	}
	result.state = state
	if state != nil {
		lock, err := lockDirectory(state)
		if err != nil {
			result.close()
			return nil, err
		}
		result.lock = lock
	}
	return result, nil
}

func (inst *installation) close() {
	if inst.lock != nil {
		_ = unlockFile(inst.lock)
		_ = inst.lock.Close()
	}
	if inst.state != nil {
		_ = inst.state.file.Close()
	}
	if inst.root != nil {
		_ = inst.root.file.Close()
	}
}

func (dir *directory) sync() error { return dir.file.Sync() }

func (dir *directory) open(name string, flags int, mode os.FileMode) (*os.File, error) {
	return dir.openRelative(name, flags, mode)
}

func (dir *directory) remove(name string) error {
	err := removeRelative(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func privateCopy(source io.Reader, target *directory, name string) (resultErr error) {
	destination, err := target.open(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, destination.Close()) }()
	if _, err = io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Sync()
}

func readPrivate(dir *directory, name string, maximum int64) ([]byte, error) {
	file, err := dir.open(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, ErrJournal
	}
	return content, nil
}

func writePrivate(dir *directory, name string, content []byte) error {
	temporary := name + ".tmp"
	file, err := dir.open(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		_ = dir.remove(temporary)
		return err
	}
	if err = renameRelative(fileRef{dir, temporary}, fileRef{dir, name}); err != nil {
		_ = dir.remove(temporary)
		return err
	}
	return dir.sync()
}

type transaction struct {
	inst  *installation
	value *journal
}

type fileRef struct {
	dir  *directory
	name string
}
