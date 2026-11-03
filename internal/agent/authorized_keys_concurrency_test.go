package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentAuthorizedKeyInstallsPreserveEveryKey(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(path, []byte(strings.Repeat("# existing entry\n", 1<<14)), 0o600); err != nil {
		t.Fatal(err)
	}
	const keyCount = 16
	start := make(chan struct{})
	errorsSeen := make(chan error, keyCount)
	var workers sync.WaitGroup
	for index := range keyCount {
		workers.Go(func() {
			<-start
			errorsSeen <- installAuthorizedKey(home, fmt.Sprintf("ssh-ed25519 generated-%02d", index))
		})
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for index := range keyCount {
		key := fmt.Sprintf("ssh-ed25519 generated-%02d", index)
		if !strings.Contains(string(content), key) {
			t.Fatalf("missing %q after concurrent installs", key)
		}
	}
	temporary, err := filepath.Glob(filepath.Join(sshDir, ".authorized_keys-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary files were not cleaned up: %v", temporary)
	}
}

func TestReplaceAuthorizedKeysRejectsMissingDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "missing", "authorized_keys")
	if err := replaceAuthorizedKeys(target, []byte("key\n")); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestReplaceAuthorizedKeysCleansTemporaryFileOnRenameFailure(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "authorized_keys")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := replaceAuthorizedKeys(target, []byte("key\n")); err == nil {
		t.Fatal("directory target accepted")
	}
	temporary, err := filepath.Glob(filepath.Join(directory, ".authorized_keys-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary files were not cleaned up: %v", temporary)
	}
}
