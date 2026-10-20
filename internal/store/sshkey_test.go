package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/beat/backend/internal/testutil"
)

func TestListSSHKeys(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := newTestSSHKeyStore(t, store.DB)

	data := []struct {
		id, name, keyType, publicKey, privateKey, fingerprint string
	}{
		{"key-1", "key-one", "rsa", "public-one", "private-one", "fp-one"},
		{"key-2", "key-two", "ed25519", "public-two", "private-two", "fp-two"},
	}
	for _, d := range data {
		_, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: d.name, KeyType: d.keyType, PublicKey: d.publicKey, PrivateKey: d.privateKey, Fingerprint: d.fingerprint})
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
	sshStore := newTestSSHKeyStore(t, store.DB)

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
	sshStore := newTestSSHKeyStore(t, store.DB)

	key, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "my-key", KeyType: "rsa", PublicKey: "public-data", PrivateKey: "private-data", Fingerprint: "aa:bb:cc"})
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
	sshStore := newTestSSHKeyStore(t, store.DB)

	created, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "lookup", KeyType: "ed25519", PublicKey: "ssh-ed25519 public", PrivateKey: "private", Fingerprint: "fingerprint"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	key, err := sshStore.GetSSHKeyByPublicKey(ctx, created.PublicKey)
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if key == nil || key.ID != created.ID || key.PrivateKey != "" {
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

func newTestSSHKeyStore(t *testing.T, database *sql.DB) *SSHKeyStore {
	t.Helper()
	keys := NewSSHKeyStore(database)
	keys.SetSecrets(testutil.NewSecrets(t, database))
	return keys
}

func TestDeleteSSHKey(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	sshStore := newTestSSHKeyStore(t, store.DB)

	key, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "delete-me", KeyType: "rsa", PublicKey: "pub", PrivateKey: "priv", Fingerprint: "fp"})
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
	sshStore := newTestSSHKeyStore(t, store.DB)

	err := sshStore.DeleteSSHKey(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetSSHKeyByPublicKeyTrimmedMatch(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshStore := newTestSSHKeyStore(t, s.DB)
	key, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "trim-key", KeyType: "ed25519", PublicKey: "ssh-ed25519 AAA\n", PrivateKey: "private", Fingerprint: "fp"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	got, err := sshStore.GetSSHKeyByPublicKey(ctx, "ssh-ed25519 AAA")
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if got == nil || got.ID != key.ID {
		t.Fatalf("key = %#v, want id %s", got, key.ID)
	}
}

func TestGetSSHKeyByPublicKeyLegacyNewlineRow(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshStore := newTestSSHKeyStore(t, s.DB)
	// Simulate a legacy row created before public key trimming was introduced.
	key, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "legacy-key", KeyType: "ed25519", PublicKey: "ssh-ed25519 AAA\n", PrivateKey: "private", Fingerprint: "fp"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	// Overwrite the stored row with the untrimmed legacy form.
	if _, err := s.DB.ExecContext(ctx, "UPDATE ssh_keys SET public_key = 'ssh-ed25519 AAA\n' WHERE id = ?", key.ID); err != nil {
		t.Fatalf("update legacy row: %v", err)
	}
	got, err := sshStore.GetSSHKeyByPublicKey(ctx, "ssh-ed25519 AAA")
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if got == nil || got.ID != key.ID {
		t.Fatalf("key = %#v, want id %s", got, key.ID)
	}
}

func TestGetSSHKeyByIDReturnsStoredKeyAndNilWhenMissing(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()
	sshStore := newTestSSHKeyStore(t, store.DB)

	created, err := sshStore.CreateSSHKey(ctx, SSHKeyInput{Name: "lookup", KeyType: "ed25519", PublicKey: "pub-lookup", PrivateKey: "priv-lookup", Fingerprint: "fp-lookup"})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	found, err := sshStore.GetSSHKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get ssh key by id: %v", err)
	}
	if found == nil || found.ID != created.ID || found.Name != "lookup" || found.Fingerprint != "fp-lookup" {
		t.Fatalf("looked up key = %#v", found)
	}
	missing, err := sshStore.GetSSHKeyByID(ctx, "missing-id")
	if err != nil {
		t.Fatalf("get missing ssh key: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing key = %#v, want nil", missing)
	}
}
