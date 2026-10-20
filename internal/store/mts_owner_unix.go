//go:build linux || darwin || freebsd

package store

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// acquireMTSOwnerLease 在打开引擎前抢占目录所有权，让第二个实例在启动期
// 就直接失败，而不是两边同时写同一份分片。
func acquireMTSOwnerLease(path string) (*mtsOwnerLease, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("create MTS store directory: %w", err)
	}
	lockPath, err := MTSOwnerLockPath(path)
	if err != nil {
		return nil, err
	}
	file, err := openMTSOwnerFile(lockPath)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, mtsOwnerBusyError(lockPath)
		}
		return nil, fmt.Errorf("lock MTS owner file: %w", err)
	}
	if err := sameMTSOwnerFile(lockPath, file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := writeMTSOwnerRecord(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &mtsOwnerLease{file: file, path: lockPath}, nil
}

func openMTSOwnerFile(path string) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CREAT | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	fd, err := unix.Open(path, flags, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open MTS owner file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat MTS owner file: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o7777 != 0o600 ||
		info.Nlink != 1 || info.Uid != uint32(os.Geteuid()) {
		_ = file.Close()
		return nil, errors.New("MTS owner file has unsafe type, permissions or owner")
	}
	return file, nil
}
