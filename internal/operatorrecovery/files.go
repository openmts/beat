package operatorrecovery

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ErrPrivatePath = errors.New("recovery requires owned private regular files and directories without symlinks")

func privatePath(path string, directory bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if resolved != absolute {
		return ErrPrivatePath
	}
	var info unix.Stat_t
	if err := unix.Lstat(absolute, &info); err != nil {
		return err
	}
	expected := uint32(unix.S_IFREG)
	permission := uint32(0o600)
	if directory {
		expected, permission = unix.S_IFDIR, 0o700
	}
	if info.Mode&unix.S_IFMT != expected || info.Mode&0o7777 != permission || info.Uid != uint32(os.Geteuid()) {
		return ErrPrivatePath
	}
	return nil
}

func openPrivate(path string) (*os.File, error) {
	if err := privatePath(path, false); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	actual, err := file.Stat()
	expected, statErr := os.Lstat(path)
	if err != nil || statErr != nil || !os.SameFile(actual, expected) {
		_ = file.Close()
		return nil, ErrPrivatePath
	}
	return file, nil
}

func copyPrivate(source, destination string) (resultErr error) {
	input, err := openPrivate(source)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func validateDirectory(dataDir string) error {
	if err := privatePath(dataDir, true); err != nil {
		return err
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(dataDir, &fs); err != nil {
		return err
	}
	if !localFilesystem(fs.Type) {
		return errors.New("recovery requires a known local filesystem with reliable locking")
	}
	return filepath.WalkDir(dataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return privatePath(path, entry.IsDir())
	})
}

func localFilesystem(kind int64) bool {
	switch kind {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC,
		unix.TMPFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, unix.RAMFS_MAGIC, unix.F2FS_SUPER_MAGIC:
		return true
	default:
		return false
	}
}
