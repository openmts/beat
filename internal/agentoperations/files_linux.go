//go:build linux

package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func splitPath(path string) []string { return strings.Split(path, string(filepath.Separator)) }

func privateDirectory(path string, owned bool) error { return privatePath(path, true, owned) }

func privateFile(path string) error { return privatePath(path, false, true) }

func privatePath(path string, directory, owned bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ErrStorage
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if absolute != resolved {
		return ErrStorage
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return err
	}
	return validatePrivateStat(stat, directory, owned)
}

func validatePrivateStat(stat unix.Stat_t, directory, owned bool) error {
	kind, mode := uint32(unix.S_IFREG), uint32(0o600)
	if directory {
		kind, mode = unix.S_IFDIR, 0o700
	}
	if stat.Mode&unix.S_IFMT != kind || stat.Mode&0o7777 != mode {
		return ErrStorage
	}
	if owned && stat.Uid != uint32(os.Geteuid()) {
		return ErrStorage
	}
	if !directory && stat.Nlink != 1 {
		return ErrStorage
	}
	return nil
}

func lockState(path string) (func() error, error) {
	descriptor, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "operations-lock")
	if err := privateFile(path); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error { return errors.Join(unix.Flock(descriptor, unix.LOCK_UN), file.Close()) }, nil
}

func replacePrivate(source, destination string) error { return os.Rename(source, destination) }

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return ErrStorage
	}
	return errors.Join(file.Sync(), file.Close())
}

func secureCreatedDirectory(path string) error { return privateDirectory(path, true) }

func platformDeniedRoots() []string { return []string{"/proc", "/sys", "/dev"} }

func validateOperationDirectory(path string) error { return privateDirectory(path, false) }

func checkSensitiveFile(string) error { return nil }

func secureCreatedFile(file *os.File) error { return file.Chmod(0o600) }
