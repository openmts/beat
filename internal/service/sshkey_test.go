package service

import (
	"context"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func setupTestSSHKeyService(t *testing.T) *SSHKeyService {
	t.Helper()

	sqliteStore, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	sshKeyStore := store.NewSSHKeyStore(sqliteStore.DB)
	svc := NewSSHKeyService(sshKeyStore)
	return svc
}

func TestGenerateKeyPairRSA(t *testing.T) {
	svc := setupTestSSHKeyService(t)

	privKey, pubKey, fingerprint, err := svc.GenerateKeyPair("rsa")
	if err != nil {
		t.Fatalf("GenerateKeyPair(rsa) error: %v", err)
	}

	if !strings.HasPrefix(privKey, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Errorf("private key should start with RSA PEM header, got: %q", privKey[:min(len(privKey), 50)])
	}
	if !strings.HasPrefix(pubKey, "ssh-rsa") {
		t.Errorf("public key should start with ssh-rsa, got: %q", pubKey[:min(len(pubKey), 50)])
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Errorf("fingerprint should start with SHA256:, got: %q", fingerprint)
	}

	if privKey == "" {
		t.Error("private key should not be empty")
	}
	if pubKey == "" {
		t.Error("public key should not be empty")
	}
	if fingerprint == "" {
		t.Error("fingerprint should not be empty")
	}
}

func TestGenerateKeyPairEd25519(t *testing.T) {
	svc := setupTestSSHKeyService(t)

	privKey, pubKey, fingerprint, err := svc.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("GenerateKeyPair(ed25519) error: %v", err)
	}

	if !strings.HasPrefix(privKey, "-----BEGIN PRIVATE KEY-----") {
		t.Errorf("private key should start with BEGIN PRIVATE KEY, got: %q", privKey[:min(len(privKey), 50)])
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519") {
		t.Errorf("public key should start with ssh-ed25519, got: %q", pubKey[:min(len(pubKey), 50)])
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Errorf("fingerprint should start with SHA256:, got: %q", fingerprint)
	}
}

func TestGenerateKeyPairUnsupported(t *testing.T) {
	svc := setupTestSSHKeyService(t)

	_, _, _, err := svc.GenerateKeyPair("unknown")
	if err == nil {
		t.Fatal("expected error for unsupported key type, got nil")
	}
}

func TestCreateKey(t *testing.T) {
	svc := setupTestSSHKeyService(t)
	ctx := context.Background()

	key, err := svc.CreateKey(ctx, "test-key", "rsa")
	if err != nil {
		t.Fatalf("CreateKey error: %v", err)
	}

	if key == nil {
		t.Fatal("CreateKey returned nil")
	}
	if key.ID == "" {
		t.Error("key ID should not be empty")
	}
	if key.Name != "test-key" {
		t.Errorf("key name = %q, want %q", key.Name, "test-key")
	}
	if key.KeyType != "rsa" {
		t.Errorf("key type = %q, want %q", key.KeyType, "rsa")
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-rsa") {
		t.Error("public key should start with ssh-rsa")
	}
	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Error("fingerprint should start with SHA256:")
	}
	if key.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
}

func TestListKeys(t *testing.T) {
	svc := setupTestSSHKeyService(t)
	ctx := context.Background()

	_, err := svc.CreateKey(ctx, "key1", "rsa")
	if err != nil {
		t.Fatalf("CreateKey(key1) error: %v", err)
	}
	_, err = svc.CreateKey(ctx, "key2", "ed25519")
	if err != nil {
		t.Fatalf("CreateKey(key2) error: %v", err)
	}
	_, err = svc.CreateKey(ctx, "key3", "rsa")
	if err != nil {
		t.Fatalf("CreateKey(key3) error: %v", err)
	}

	keys, err := svc.ListKeys(ctx)
	if err != nil {
		t.Fatalf("ListKeys error: %v", err)
	}

	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func TestDeleteKey(t *testing.T) {
	svc := setupTestSSHKeyService(t)
	ctx := context.Background()

	key, err := svc.CreateKey(ctx, "test-key", "rsa")
	if err != nil {
		t.Fatalf("CreateKey error: %v", err)
	}

	keys, err := svc.ListKeys(ctx)
	if err != nil {
		t.Fatalf("ListKeys before delete error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key before delete, got %d", len(keys))
	}

	err = svc.DeleteKey(ctx, key.ID)
	if err != nil {
		t.Fatalf("DeleteKey error: %v", err)
	}

	keys, err = svc.ListKeys(ctx)
	if err != nil {
		t.Fatalf("ListKeys after delete error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after delete, got %d", len(keys))
	}
}
