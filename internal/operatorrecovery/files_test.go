package operatorrecovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivatePathsAndEmptyInstances(t *testing.T) {
	directory := privateTemp(t)
	config := Config{DataDir: directory}
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status.State != "empty_instance" {
		t.Fatal("missing empty status")
	}
	if _, err := session.SelectOwner("owner"); err == nil {
		t.Fatal("invented owner")
	}
	if err := session.StageRestore(t.Context(), "/absent"); err == nil {
		t.Fatal("missing archive accepted")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "unknown")
	if err := os.WriteFile(file, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("unknown nonempty accepted")
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := privatePath(file, false); err == nil {
		t.Fatal("public file accepted")
	}
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := privatePath(file, false); err == nil {
		t.Fatal("read-only file accepted")
	}
	link := filepath.Join(privateTemp(t), "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if err := privatePath(link, true); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := resolveConfig(Config{}); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := resolveConfig(Config{DataDir: directory, DatabasePath: "/outside.db"}); err == nil {
		t.Fatal("outside DB accepted")
	}
	if err := CreateEmptyDirectory(filepath.Join(privateTemp(t), "new")); err != nil {
		t.Fatal(err)
	}
	if err := CreateEmptyDirectory(file); err == nil {
		t.Fatal("file used as directory")
	}
}

func TestPrivateCopyAndPathFailures(t *testing.T) {
	root := privateTemp(t)
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.WriteFile(source, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivate(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivate(source, destination); err == nil {
		t.Fatal("overwrote destination")
	}
	if err := copyPrivate("/absent", destination); err == nil {
		t.Fatal("missing source accepted")
	}
	if err := validateDirectory(source); err == nil {
		t.Fatal("file used as directory")
	}
	if _, err := Open(t.Context(), Config{DataDir: "/absent"}); err == nil {
		t.Fatal("absent directory accepted")
	}
	if err := CreateEmptyDirectory(filepath.Join(root, "absent", "nested")); err == nil {
		t.Fatal("created parent implicitly")
	}
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateDirectory(root); err == nil {
		t.Fatal("public child accepted")
	}
}

func TestFilesystemAndOwnershipAuthority(t *testing.T) {
	for _, kind := range []int64{0, 0x6969, 0xff534d42} {
		if localFilesystem(kind) {
			t.Fatal("unknown or remote filesystem accepted")
		}
	}
	if !localFilesystem(0xef53) {
		t.Fatal("ext4 rejected")
	}
	if os.Geteuid() == 0 {
		path := filepath.Join(privateTemp(t), "foreign")
		if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if err := privatePath(path, false); err == nil {
			t.Fatal("wrong ownership accepted")
		}
	}
}

func privateTemp(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}
