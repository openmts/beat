package secretbox

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerKeyLifecycleAndEncryption(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "security", "admin-data.key")
	manager, err := New(keyPath, bytes.NewReader(bytes.Repeat([]byte{1}, 96)))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", info.Mode().Perm())
	}
	ciphertext, err := manager.Encrypt([]byte("totp-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plaintext, err := manager.Decrypt(ciphertext)
	if err != nil || string(plaintext) != "totp-secret" {
		t.Fatalf("plaintext = %q, err = %v", plaintext, err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := manager.Decrypt(ciphertext); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
}

func TestManagerRejectsInvalidKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "admin-data.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if _, err := New(keyPath, bytes.NewReader(nil)); err == nil {
		t.Fatal("invalid key file accepted")
	}
}

func TestManagerRejectsInvalidCiphertextAndRandomFailure(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "admin-data.key")
	manager, err := New(keyPath, bytes.NewReader(bytes.Repeat([]byte{2}, keyLength)))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := manager.Decrypt([]byte("short")); err == nil {
		t.Fatal("short ciphertext decrypted")
	}
	manager.random = errorReader{}
	if _, err := manager.Encrypt([]byte("secret")); err == nil {
		t.Fatal("encryption with failed random source succeeded")
	}
}

func TestKeyFileErrorPaths(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root, bytes.NewReader(nil)); err == nil {
		t.Fatal("directory accepted as administrator key file")
	}

	blockedParent := filepath.Join(root, "parent-file")
	if err := os.WriteFile(blockedParent, []byte("file"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if _, err := createKey(filepath.Join(blockedParent, "key"), bytes.NewReader(nil)); err == nil {
		t.Fatal("key created below a regular file")
	}

	if _, err := createKey(filepath.Join(root, "random-error", "key"), errorReader{}); err == nil {
		t.Fatal("key created with failed random source")
	}

	existing := filepath.Join(root, "existing-key")
	if err := os.WriteFile(existing, bytes.Repeat([]byte{4}, keyLength), 0o600); err != nil {
		t.Fatalf("write existing key: %v", err)
	}
	if _, err := createKey(existing, bytes.NewReader(bytes.Repeat([]byte{5}, keyLength))); err == nil {
		t.Fatal("exclusive key creation replaced an existing key")
	}

	loaded, err := loadOrCreateKey(existing, bytes.NewReader(nil))
	if err != nil || !bytes.Equal(loaded, bytes.Repeat([]byte{4}, keyLength)) {
		t.Fatalf("load existing key = %x, %v", loaded, err)
	}
}

func TestManagerUsesDefaultRandomSource(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "admin-data.key")
	manager, err := New(keyPath, nil)
	if err != nil {
		t.Fatalf("new manager with default random source: %v", err)
	}
	if manager.random == nil {
		t.Fatal("default random source was not assigned")
	}
	plaintext, err := manager.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("encrypt with default random source: %v", err)
	}
	decrypted, err := manager.Decrypt(plaintext)
	if err != nil || string(decrypted) != "secret" {
		t.Fatalf("round trip = %q, %v", decrypted, err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

var _ io.Reader = errorReader{}
