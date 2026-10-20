package applicationsecrets

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var ErrLifecycleBusy = errors.New("application secret lifecycle is busy")

// Lease 位于可替换数据目录之外，服务全生命周期内持有且从不删除锁文件。
type Lease struct {
	file *os.File
	once sync.Once
	err  error
}

func LifecycleLockPath(dataDir string) (string, error) {
	canonical, err := canonicalDataPath(dataDir)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(filepath.Dir(canonical), fmt.Sprintf(".beat-lifecycle-%x.lock", digest)), nil
}

func canonicalDataPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("data directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve data directory parent: %w", err)
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func AcquireLease(dataDir string) (*Lease, error) {
	path, err := LifecycleLockPath(dataDir)
	if err != nil {
		return nil, err
	}
	owner, err := lifecycleOwner(dataDir)
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path, unix.O_RDWR|unix.O_CREAT, owner)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrLifecycleBusy
		}
		return nil, fmt.Errorf("lock application lifecycle: %w", err)
	}
	if err := samePrivateFile(path, file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Lease{file: file}, nil
}

func lifecycleOwner(dataDir string) (uint32, error) {
	var info unix.Stat_t
	if err := unix.Stat(dataDir, &info); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return uint32(os.Geteuid()), nil
		}
		return 0, fmt.Errorf("inspect lifecycle directory: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		return 0, errors.New("data path is not a directory")
	}
	return info.Uid, nil
}

func (lease *Lease) Close() error {
	lease.once.Do(func() {
		lease.err = errors.Join(unix.Flock(int(lease.file.Fd()), unix.LOCK_UN), lease.file.Close())
	})
	return lease.err
}
