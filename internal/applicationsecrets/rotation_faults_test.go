package applicationsecrets

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

func TestRootRotationPreservesEvidenceAfterFilePublicationFailures(t *testing.T) {
	for phase, name := range map[string]string{"root_prepare_created": "old.key", "root_old_saved": "next.key", "root_next_saved": "journal.json"} {
		t.Run(phase, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
			before, err := readRootKey(filepath.Join(directory, "admin-data.key"))
			if err != nil {
				t.Fatal(err)
			}
			config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"), fault: func(current string) error {
				if current == phase {
					return os.WriteFile(filepath.Join(directory, rotationPrepareDir, name), []byte("collision"), 0o600)
				}
				return nil
			}}
			if err := rotateRoot(t.Context(), config); err == nil {
				t.Fatal("publication collision overwritten")
			}
			if err := RecoverRootFiles(t.Context(), directory, config.DatabasePath); err != nil {
				t.Fatal(err)
			}
			after, err := readRootKey(filepath.Join(directory, "admin-data.key"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed preparation changed root")
			}
		})
	}
}

func TestOfflineRotationEntropyFailuresAreRecoverable(t *testing.T) {
	for _, available := range []int{0, rootKeyBytes} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
			config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"), Random: bytes.NewReader(bytes.Repeat([]byte{4}, available))}
			if err := rotateRoot(t.Context(), config); err == nil {
				t.Fatal("insufficient rotation entropy accepted")
			}
			if err := RecoverRootFiles(t.Context(), directory, config.DatabasePath); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOnlineRotationRejectsLostInventoryAndRetirementWrites(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	now := time.Now()
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.fault = func(phase string) error {
		if phase == "rotation_before_activate" {
			return errors.New("activation interrupted")
		}
		return nil
	}
	if _, err := service.StartRotation(t.Context()); err == nil {
		t.Fatal("activation interruption ignored")
	}
	service.fault = nil
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	now = now.Add(keyRetention + time.Second)
	if _, err := database.DB.Exec("CREATE TRIGGER deny_retirement BEFORE DELETE ON application_data_keys BEGIN SELECT RAISE(ABORT,'denied'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(t.Context()); err == nil {
		t.Fatal("retirement failure ignored")
	}
	if _, err := database.DB.Exec("DROP TABLE application_data_keys"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(t.Context()); err == nil {
		t.Fatal("missing registry ignored")
	}
}

func TestRegistryValidationRejectsImpossibleStates(t *testing.T) {
	state := lifecycleState{Phase: "ready", Generation: 2, ActiveID: "active"}
	for _, key := range []registryKey{
		{WrappedDataKey: secretbox.WrappedDataKey{ID: "active", Generation: 2}, CreatedAt: 1, ActivatedAt: 1, State: "active", RetireAfter: sql.NullInt64{Valid: true, Int64: 2}},
		{WrappedDataKey: secretbox.WrappedDataKey{ID: "active", Generation: 1}, CreatedAt: 1, ActivatedAt: 1, State: "decrypt_only"},
		{WrappedDataKey: secretbox.WrappedDataKey{ID: "old", Generation: 1}, CreatedAt: 1, ActivatedAt: 1, State: "unknown"},
		{WrappedDataKey: secretbox.WrappedDataKey{ID: "old", Generation: 1}, CreatedAt: 1, ActivatedAt: 1, State: "decrypt_only"},
	} {
		if err := validateRegistryKey(key, state); err == nil {
			t.Fatal("impossible registry state accepted")
		}
	}
}

func TestMigratingNonceFailureDoesNotClearLegacyCredential(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	root := bytes.Repeat([]byte{7}, rootKeyBytes)
	seedLegacyTOTP(t, database, root)
	if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, Random: bytes.NewReader(bytes.Repeat([]byte{4}, 60))}); err == nil {
		t.Fatal("failed nonce accepted")
	}
	var ciphertext []byte
	if err := database.DB.QueryRow("SELECT totp_secret_encrypted FROM admin_users").Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	manager, err := secretbox.NewFromKey(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Decrypt(ciphertext); err != nil {
		t.Fatal("legacy secret cleared by failed encryption")
	}
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
}
