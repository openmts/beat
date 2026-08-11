//go:build linux

package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSQLiteFilesUsePrivatePermissions(t *testing.T) {
	previousMask := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(previousMask) })

	tests := []struct {
		name        string
		connection  func(string) string
		initialMode os.FileMode
	}{
		{name: "filesystem path", connection: func(path string) string { return path }},
		{name: "file URI", connection: func(path string) string { return "file:" + path + "?cache=shared" }},
		{name: "existing permissive file", connection: func(path string) string { return path }, initialMode: 0o666},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private.db")
			if test.initialMode != 0 {
				if err := os.WriteFile(path, nil, test.initialMode); err != nil {
					t.Fatalf("create permissive SQLite file: %v", err)
				}
				if err := os.Chmod(path, test.initialMode); err != nil {
					t.Fatalf("set permissive SQLite mode: %v", err)
				}
			}
			sqliteStore, err := NewSQLiteStore(test.connection(path))
			if err != nil {
				t.Fatalf("open SQLite store: %v", err)
			}
			t.Cleanup(func() { _ = sqliteStore.Close() })

			assertSQLiteFileModes(t, path)
		})
	}
}

func assertSQLiteFileModes(t *testing.T, path string) {
	t.Helper()
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatalf("stat %s: %v", candidate, err)
		}
		if permission := info.Mode().Perm(); permission != 0o600 {
			t.Errorf("%s permissions = %04o, want 0600", candidate, permission)
		}
	}
}
