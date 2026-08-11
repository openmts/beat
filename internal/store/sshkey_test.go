package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestListSSHKeys(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	data := []struct {
		id, name, keyType, publicKey, privateKey, fingerprint string
	}{
		{"key-1", "key-one", "rsa", "public-one", "private-one", "fp-one"},
		{"key-2", "key-two", "ed25519", "public-two", "private-two", "fp-two"},
	}
	for _, d := range data {
		_, err := store.DB.ExecContext(ctx,
			"INSERT INTO ssh_keys (id, name, key_type, public_key, private_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			d.id, d.name, d.keyType, d.publicKey, d.privateKey, d.fingerprint, model.NowUTC(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	keys, err := sshStore.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestListSSHKeysEmpty(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	keys, err := sshStore.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestCreateSSHKey(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	key, err := sshStore.CreateSSHKey(ctx, "my-key", "rsa", "public-data", "private-data", "aa:bb:cc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected key, got nil")
	}
	if key.Name != "my-key" {
		t.Errorf("expected name %q, got %q", "my-key", key.Name)
	}
	if key.KeyType != "rsa" {
		t.Errorf("expected key_type %q, got %q", "rsa", key.KeyType)
	}
	if key.PublicKey != "public-data" {
		t.Errorf("expected public_key %q, got %q", "public-data", key.PublicKey)
	}
	if key.PrivateKey != "private-data" {
		t.Errorf("expected private_key %q, got %q", "private-data", key.PrivateKey)
	}
	if key.Fingerprint != "aa:bb:cc" {
		t.Errorf("expected fingerprint %q, got %q", "aa:bb:cc", key.Fingerprint)
	}
	if key.ID == "" {
		t.Error("expected non-empty key ID")
	}
}

func TestGetSSHKeyByPublicKey(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	created, err := sshStore.CreateSSHKey(ctx, "lookup", "ed25519", "ssh-ed25519 public", "private", "fingerprint")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	key, err := sshStore.GetSSHKeyByPublicKey(ctx, created.PublicKey)
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if key == nil || key.ID != created.ID || key.PrivateKey != "private" {
		t.Fatalf("key = %#v, want created key", key)
	}

	missing, err := sshStore.GetSSHKeyByPublicKey(ctx, "missing")
	if err != nil {
		t.Fatalf("get missing key: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing key = %#v, want nil", missing)
	}
}

func TestDeleteSSHKey(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	key, err := sshStore.CreateSSHKey(ctx, "delete-me", "rsa", "pub", "priv", "fp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = sshStore.DeleteSSHKey(ctx, key.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := sshStore.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after delete, got %d", len(keys))
	}
}

func TestDeleteSSHKeyNonExistent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := NewSSHKeyStore(store.DB)

	err := sshStore.DeleteSSHKey(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
