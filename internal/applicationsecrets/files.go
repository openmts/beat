package applicationsecrets

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const rootKeyBytes = 32

func openPrivateFile(path string, flags int, owner uint32) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open private lifecycle file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o7777 != 0o600 || info.Nlink != 1 || info.Uid != owner {
		_ = file.Close()
		return nil, errors.New("private lifecycle file has unsafe type, permissions or owner")
	}
	return file, nil
}

func samePrivateFile(path string, file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(opened, current) {
		return errors.New("private lifecycle file changed while opening")
	}
	return nil
}

func readRootKey(path string) (key []byte, resultErr error) {
	owner, err := privateDirectoryOwner(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path, unix.O_RDONLY, owner)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	key, err = io.ReadAll(io.LimitReader(file, rootKeyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read application root key: %w", err)
	}
	if len(key) != rootKeyBytes {
		clear(key)
		return nil, errors.New("application root key must contain 32 bytes")
	}
	if err := samePrivateFile(path, file); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

func privateDirectoryOwner(path string) (uint32, error) {
	var info unix.Stat_t
	if err := unix.Lstat(path, &info); err != nil {
		return 0, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o7777 != 0o700 {
		return 0, errors.New("application secret directory must be a private regular directory")
	}
	if os.Geteuid() != 0 && info.Uid != uint32(os.Geteuid()) {
		return 0, errors.New("application secret directory has an unexpected owner")
	}
	return info.Uid, nil
}

func publishNewPrivateFile(path string, value []byte) (resultErr error) {
	owner, err := privateDirectoryOwner(filepath.Dir(path))
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".beat-secret-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if os.Geteuid() == 0 {
		if err := file.Chown(int(owner), -1); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := writeSyncedFile(file, value); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, file.Name(), unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("publish private lifecycle file: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func writeSyncedFile(file *os.File, value []byte) error {
	_, writeErr := file.Write(value)
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
