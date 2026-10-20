//go:build linux

package agentupdate

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openDirectory(path string) (*directory, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrStorage
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		closeErr := unix.Close(fd)
		if openErr != nil || closeErr != nil {
			if next >= 0 {
				_ = unix.Close(next)
			}
			return nil, ErrUnsafePath
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, ErrStorage
	}
	if !safeInstallationMode(stat) {
		_ = file.Close()
		return nil, ErrManualOnly
	}
	return &directory{file: file, device: uint64(stat.Dev)}, nil
}

func openChildDirectory(parent *directory, name string, create bool) (*directory, error) {
	if create {
		if err := unix.Mkdirat(int(parent.file.Fd()), name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, ErrStorage
		}
	}
	fd, err := unix.Openat(int(parent.file.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) && !create {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnsafePath
	}
	file := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, ErrStorage
	}
	return admitChildDirectory(parent, &directory{file: file, device: uint64(stat.Dev)}, childAdmission{stat: stat, create: create})
}

type childAdmission struct {
	stat   unix.Stat_t
	create bool
}

func admitChildDirectory(parent, child *directory, admission childAdmission) (*directory, error) {
	file, stat, create := child.file, admission.stat, admission.create
	var err error
	if uint64(stat.Dev) != parent.device {
		_ = file.Close()
		return nil, ErrCrossDevice
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o777 != 0o700 {
		_ = file.Close()
		return nil, ErrUnsafePath
	}
	if create {
		if err = parent.sync(); err != nil {
			_ = file.Close()
			return nil, ErrStorage
		}
	}
	return &directory{file: file, device: uint64(stat.Dev)}, nil
}

func (dir *directory) openRelative(name string, flags int, mode os.FileMode) (*os.File, error) {
	if name == "" || strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return nil, ErrUnsafePath
	}
	fd, err := unix.Openat(int(dir.file.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, ErrStorage
	}
	if uint64(stat.Dev) != dir.device {
		_ = file.Close()
		return nil, ErrCrossDevice
	}
	if !safeRegular(stat) {
		_ = file.Close()
		return nil, ErrUnsafePath
	}
	return file, nil
}

func renameRelative(source, target fileRef) error {
	from, old, to, name := source.dir, source.name, target.dir, target.name
	if from.device != to.device {
		return ErrCrossDevice
	}
	return unix.Renameat(int(from.file.Fd()), old, int(to.file.Fd()), name)
}

func removeRelative(dir *directory, name string) error {
	return unix.Unlinkat(int(dir.file.Fd()), name, 0)
}

func lockDirectory(dir *directory) (*os.File, error) {
	file, err := dir.open("lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, ErrBusy
	}
	return file, nil
}

func unlockFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }

func availableSpace(dir *directory) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(dir.file.Fd()), &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || stat.Bavail > ^uint64(0)/uint64(stat.Bsize) {
		return 0, ErrStorage
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func safeRegular(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid()) && stat.Mode&0o022 == 0
}

func safeInstallationMode(stat unix.Stat_t) bool {
	return stat.Uid == uint32(os.Geteuid()) && stat.Mode&0o700 == 0o700 && stat.Mode&0o022 == 0
}
