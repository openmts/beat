package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteReplacesAndCleansTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.json")
	for _, value := range []string{"first", "second"} {
		if err := atomicWrite(path, []byte(value), privateFile); err != nil {
			t.Fatal(err)
		}
		data, err := readPrivate(path)
		if err != nil || string(data) != value {
			t.Fatalf("want %q, got %q: %v", value, data, err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
	assertMode(t, path, privateFile)
}

func TestAtomicWriteFailureCleansTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := atomicWrite(filepath.Join(root, "absent", "config"), nil, privateFile); err == nil {
		t.Fatal("expected missing-parent failure")
	}
	directory := filepath.Join(root, "directory")
	mustMkdir(t, directory)
	if err := atomicWrite(directory, nil, privateFile); err == nil {
		t.Fatal("expected rename failure")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
	if err := syncDirectory(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected missing-directory failure")
	}
}

func TestPrivateFileChecks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state")
	if _, err := readPrivate(path); !os.IsNotExist(err) {
		t.Fatalf("want missing file, got %v", err)
	}
	mustWrite(t, path, []byte(strings.Repeat("a", maxPrivateFile+1)), privateFile)
	if _, err := readPrivate(path); err == nil {
		t.Fatal("expected file limit")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivate(path); err == nil {
		t.Fatal("expected private mode failure")
	}
	if err := checkAncestors("relative"); err == nil {
		t.Fatal("expected absolute path validation")
	}
	if err := checkAncestors(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("want missing directory, got %v", err)
	}
	if err := checkFile(filepath.Join(path, "child"), privateFile); err == nil {
		t.Fatal("expected unsafe ancestor")
	}
}

func TestDirectoryChecksAndCreation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one", "two")
	if err := ensureDirectory(path, true); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, privateDirectory)
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := checkAncestors(path); err == nil {
		t.Fatal("expected writable directory rejection")
	}
	if err := ensureDirectory(filepath.Join(path, "child"), true); err == nil {
		t.Fatal("expected unsafe parent rejection")
	}
	file := filepath.Join(root, "file")
	mustWrite(t, file, nil, privateFile)
	if err := ensureDirectory(filepath.Join(file, "child"), false); err == nil {
		t.Fatal("expected non-directory parent")
	}
}

func TestLockRejectsSymlinksAndInsecurePermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lock")
	target := filepath.Join(root, "target")
	mustWrite(t, target, nil, privateFile)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireLock(path); err == nil {
		_ = release()
		t.Fatal("expected symlink rejection")
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireLock(target); err == nil {
		_ = release()
		t.Fatal("expected mode rejection")
	}
}

func TestExecutableChecks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent")
	mustWrite(t, path, []byte("binary"), privateFile)
	if _, err := readExecutable(path); err == nil {
		t.Fatal("expected non-executable rejection")
	}
	if err := os.Chmod(path, executableFile); err != nil {
		t.Fatal(err)
	}
	if data, err := readExecutable(path); err != nil || string(data) != "binary" {
		t.Fatalf("read executable: %q, %v", data, err)
	}
}

func TestRecordedBinaryCanRecoverAfterSourceUpgrade(t *testing.T) {
	f := newFixture(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	source, err := f.installer.executable()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, source, []byte("new agent binary"), executableFile)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f.installer.paths.binary, []byte("unmanaged replacement"), executableFile)
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected changed installed binary rejection")
	}
}

func TestSaveJSONAndLoadPrivateErrors(t *testing.T) {
	f := newFixture(t)
	if err := f.installer.saveJSON("irrelevant", make(chan int)); err == nil {
		t.Fatal("expected unsupported JSON value")
	}
	var record installation
	if found, err := loadPrivateJSON(filepath.Join(t.TempDir(), "missing"), &record); found || err != nil {
		t.Fatal("missing state should be optional")
	}
	root := t.TempDir()
	path := filepath.Join(root, "state")
	mustWrite(t, path, []byte(`{}`), 0o644)
	if _, err := loadPrivateJSON(path, &record); err == nil {
		t.Fatal("expected unsafe state error")
	}
	f.installer.lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := f.installer.systemctl(context.Background(), "show"); err == nil {
		t.Fatal("expected systemctl missing error")
	}
}

func TestAdoptedExecutableUsesPrivateMode(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Dir(f.installer.paths.binary))
	source, err := f.installer.executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f.installer.paths.binary, data, 0o755)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	assertMode(t, f.installer.paths.binary, executableFile)
}
