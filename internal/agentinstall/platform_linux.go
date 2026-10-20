package agentinstall

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || stat.Uid == uint32(os.Geteuid()))
}

func acquireLock(path string) (func() error, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, privateFile)
	if err != nil {
		return nil, err
	}
	if err := checkFile(path, privateFile); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf("another beat-agent installation is running: %w", err), file.Close())
	}
	return file.Close, nil
}
