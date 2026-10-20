package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openOwnerTestStore(t *testing.T, path string) *MTSStore {
	t.Helper()
	store, err := NewMTSStore(path)
	if err != nil {
		t.Fatalf("open MTS store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMTSStoreRejectsSecondOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mts")
	openOwnerTestStore(t, path)

	second, err := NewMTSStore(path)
	if second != nil {
		_ = second.Close()
		t.Fatal("second store opened a directory an owner already holds")
	}
	if !errors.Is(err, ErrMTSStoreBusy) {
		t.Fatalf("second open error = %v, want %v", err, ErrMTSStoreBusy)
	}
	if !strings.Contains(err.Error(), "pid") {
		t.Fatalf("second open error = %v, want holder pid detail", err)
	}
}

func TestMTSStoreOwnerReleasedOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mts")
	first := openOwnerTestStore(t, path)
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second, err := NewMTSStore(path)
	if err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}
}

func TestMTSStoreOwnerLockFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mts")
	openOwnerTestStore(t, path)
	lockPath, err := MTSOwnerLockPath(path)
	if err != nil {
		t.Fatalf("owner lock path: %v", err)
	}
	if filepath.Dir(lockPath) != filepath.Dir(path) {
		t.Fatalf("owner lock path = %s, want sibling of the MTS directory", lockPath)
	}
	info, err := os.Lstat(lockPath)
	if err != nil {
		t.Fatalf("stat owner lock: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("owner lock mode = %v, want regular 0600", info.Mode())
	}
}

func TestMTSStoreOwnerLockPathResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "mts")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatalf("create MTS directory: %v", err)
	}
	link := filepath.Join(root, "mts-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("create MTS symlink: %v", err)
	}

	direct, err := MTSOwnerLockPath(real)
	if err != nil {
		t.Fatalf("owner lock path: %v", err)
	}
	aliased, err := MTSOwnerLockPath(filepath.Join(link, "."))
	if err != nil {
		t.Fatalf("aliased owner lock path: %v", err)
	}
	if direct != aliased {
		t.Fatalf("owner lock paths differ: %s vs %s", direct, aliased)
	}
}

func TestMTSStoreOwnerAcceptsStaleLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mts")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create MTS directory: %v", err)
	}
	lockPath, err := MTSOwnerLockPath(path)
	if err != nil {
		t.Fatalf("owner lock path: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"pid":999999,"started_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("write stale owner lock: %v", err)
	}
	openOwnerTestStore(t, path)
}

func TestMTSStoreOwnerRejectsUnsafeLockFile(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, string){
		"symlink": func(t *testing.T, lockPath string) {
			target := lockPath + ".target"
			if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
				t.Fatalf("write symlink target: %v", err)
			}
			if err := os.Symlink(target, lockPath); err != nil {
				t.Fatalf("create owner lock symlink: %v", err)
			}
		},
		"world-readable": func(t *testing.T, lockPath string) {
			if err := os.WriteFile(lockPath, []byte("{}"), 0o644); err != nil {
				t.Fatalf("write public owner lock: %v", err)
			}
			// umask（scripts/check.sh 设 077）会削减 WriteFile 的权限位，这里显式 Chmod
			// 才能保证磁盘上真的留着一个他人可读的 owner 锁文件。
			if err := os.Chmod(lockPath, 0o644); err != nil {
				t.Fatalf("make owner lock public: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mts")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatalf("create MTS directory: %v", err)
			}
			lockPath, err := MTSOwnerLockPath(path)
			if err != nil {
				t.Fatalf("owner lock path: %v", err)
			}
			prepare(t, lockPath)
			if store, err := NewMTSStore(path); err == nil {
				_ = store.Close()
				t.Fatal("unsafe owner lock file was accepted")
			}
		})
	}
}

func TestMTSStoreHealthFailsWhenOwnerFileRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mts")
	store := openOwnerTestStore(t, path)
	lockPath, err := MTSOwnerLockPath(path)
	if err != nil {
		t.Fatalf("owner lock path: %v", err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove owner lock: %v", err)
	}

	healthy, reasons := store.Health()
	if healthy {
		t.Fatal("health stayed ready after the owner lock file was removed")
	}
	if len(reasons) == 0 {
		t.Fatal("health reported no reason after owner loss")
	}
}
