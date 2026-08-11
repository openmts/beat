package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	return key
}

func TestKnownHostsTrustOnFirstUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh", "known_hosts")
	knownHosts, err := NewKnownHosts(path)
	if err != nil {
		t.Fatalf("create known hosts: %v", err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	firstKey := testPublicKey(t)

	if err := knownHosts.Callback("example.test:22", remote, firstKey); err != nil {
		t.Fatalf("trust first key: %v", err)
	}
	if err := knownHosts.Callback("example.test:22", remote, firstKey); err != nil {
		t.Fatalf("accept known key: %v", err)
	}
	if err := knownHosts.Callback("example.test:22", remote, testPublicKey(t)); err == nil {
		t.Fatal("expected changed host key error")
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat known hosts: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}

func TestNewKnownHostsRejectsInvalidPath(t *testing.T) {
	if _, err := NewKnownHosts("/dev/null/known_hosts"); err == nil {
		t.Fatal("expected invalid path error")
	}
	if _, err := NewKnownHosts(t.TempDir()); err == nil {
		t.Fatal("expected directory path error")
	}
}

func TestKnownHostsRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	knownHosts, err := NewKnownHosts(path)
	if err != nil {
		t.Fatalf("create known hosts: %v", err)
	}
	if err := os.WriteFile(path, []byte("invalid line\n"), 0o600); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	if err := knownHosts.Callback("example.test:22", remote, testPublicKey(t)); err == nil {
		t.Fatal("expected malformed known hosts error")
	}
}

func TestKnownHostsAppendOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	knownHosts, err := NewKnownHosts(path)
	if err != nil {
		t.Fatalf("create known hosts: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove known hosts: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("replace known hosts with directory: %v", err)
	}
	if err := knownHosts.append("example.test:22", testPublicKey(t)); err == nil {
		t.Fatal("expected append open error")
	}
}

func TestParseSignerRejectsInvalidKey(t *testing.T) {
	if _, err := parseSigner("not a private key"); err == nil {
		t.Fatal("expected private key parse error")
	}
}
