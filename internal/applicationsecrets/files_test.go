package applicationsecrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRootKeyFilesAreStrictAndCreateOnly(t *testing.T) {
	directory := privateTestDirectory(t)
	path := filepath.Join(directory, "admin-data.key")
	key := bytes.Repeat([]byte{9}, rootKeyBytes)
	if err := publishNewPrivateFile(path, key); err != nil {
		t.Fatal(err)
	}
	actual, err := readRootKey(path)
	if err != nil || !bytes.Equal(key, actual) {
		t.Fatalf("key read failed: %v", err)
	}
	if err := publishNewPrivateFile(path, bytes.Repeat([]byte{2}, rootKeyBytes)); err == nil {
		t.Fatal("replaced existing root")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("root mode: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRootKey(path); err == nil {
		t.Fatal("accepted public root")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, key[:rootKeyBytes-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRootKey(path); err == nil {
		t.Fatal("accepted truncated root")
	}
}

func TestRootKeyFileRejectsSymlinksAndMultipleLinks(t *testing.T) {
	directory := privateTestDirectory(t)
	path := filepath.Join(directory, "key")
	if err := publishNewPrivateFile(path, make([]byte, rootKeyBytes)); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readRootKey(alias); err == nil {
		t.Fatal("followed root symlink")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readRootKey(path); err == nil {
		t.Fatal("accepted root hardlink")
	}
}

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
