package applicationsecrets

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestOpenInitializesStrictRegistry(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if err := service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	aad := secretbox.TOTPSecretAAD("resource-a")
	err = service.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		var err error
		ciphertext, err = access.Seal([]byte("private value"), aad)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := service.OpenResource(t.Context(), ciphertext, aad)
	if err != nil || string(plaintext) != "private value" {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := service.OpenResource(t.Context(), ciphertext, secretbox.TOTPSecretAAD("resource-b")); err == nil {
		t.Fatal("accepted cross-resource ciphertext")
	}
	if err := service.Ready(t.Context()); err == nil {
		t.Fatal("corrupt ciphertext did not affect readiness")
	}
}

func TestMissingRootDoesNotReplaceEncryptedReferences(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	seedLegacyTOTP(t, database, bytes.Repeat([]byte{1}, rootKeyBytes))
	path := filepath.Join(directory, "admin-data.key")
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		t.Fatal("generated replacement for missing root")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root was replaced: %v", err)
	}
}

func TestLegacyBackfillSurvivesCommittedBatchInterruption(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	root := bytes.Repeat([]byte{1}, rootKeyBytes)
	seedLegacyTOTP(t, database, root)
	if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected interruption")
	_, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory,
		fault: func(phase string) error {
			if phase == "batch_committed" {
				return injected
			}
			return nil
		}})
	if !errors.Is(err, injected) {
		t.Fatalf("interruption = %v", err)
	}
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	var encrypted []byte
	if err := database.DB.QueryRow("SELECT totp_secret_encrypted FROM admin_users WHERE id = 'user-a'").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plaintext, err := service.OpenResource(t.Context(), encrypted, secretbox.TOTPSecretAAD("user-a"))
	if err != nil || string(plaintext) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("migrated TOTP: %v", err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func lifecycleTestDB(t *testing.T) (*store.SQLiteStore, string) {
	t.Helper()
	directory := privateTestDirectory(t)
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'application_secret_state'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		if err := store.MigrateApplicationSecrets(context.Background(), database.DB); err != nil {
			t.Fatal(err)
		}
	}
	return database, directory
}

func seedLegacyTOTP(t *testing.T, database *store.SQLiteStore, root []byte) {
	t.Helper()
	manager, err := secretbox.NewFromKey(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := manager.Encrypt([]byte("JBSWY3DPEHPK3PXP"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec(`INSERT INTO admin_users
		(id, username, display_name, role, password_hash, enabled, password_changed_at,
		totp_secret_encrypted, totp_enabled_at, created_at, updated_at)
		VALUES ('user-a', 'alice', 'Alice', 'owner', 'test hash', 1, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, encrypted)
	if err != nil {
		t.Fatal(err)
	}
}
