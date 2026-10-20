package applicationsecrets

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestNewWritesAndOldReadsDuringOnlineRotation(t *testing.T) {
	service, database, directory := legacyTOTPFixture(t)
	var old []byte
	if err := database.DB.QueryRow("SELECT totp_secret_encrypted FROM admin_users WHERE id='user-a'").Scan(&old); err != nil {
		t.Fatal(err)
	}
	activated, proceed := make(chan struct{}), make(chan struct{})
	continueRotation := sync.OnceFunc(func() { close(proceed) })
	t.Cleanup(continueRotation)
	service.fault = func(phase string) error {
		if phase == "rotation_activated" {
			close(activated)
			<-proceed
		}
		return nil
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-activated
	if _, err := service.AcquireBackup(t.Context()); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("backup raced rotation: %v", err)
	}
	if _, err := service.StartRotation(t.Context()); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("duplicate rotation: %v", err)
	}
	if _, err := service.OpenResource(t.Context(), old, secretbox.TOTPSecretAAD("user-a")); err != nil {
		t.Fatal(err)
	}
	keys := store.NewSSHKeyStore(database.DB)
	keys.SetSecrets(service)
	createConcurrentSSHKeys(t, keys)
	continueRotation()
	service.wg.Wait()
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func createConcurrentSSHKeys(t *testing.T, keys *store.SSHKeyStore) {
	t.Helper()
	const count = 12
	errorsSeen := make(chan error, count)
	var workers sync.WaitGroup
	for index := range count {
		workers.Go(func() {
			_, err := keys.CreateSSHKey(t.Context(), store.SSHKeyInput{Name: fmt.Sprintf("key-%d", index), KeyType: "ed25519", PublicKey: "public", PrivateKey: "private", Fingerprint: "fp"})
			errorsSeen <- err
		})
	}
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func legacyTOTPFixture(t *testing.T) (*Service, *store.SQLiteStore, string) {
	t.Helper()
	database, directory := lifecycleTestDB(t)
	root := bytes.Repeat([]byte{6}, rootKeyBytes)
	seedLegacyTOTP(t, database, root)
	if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
		t.Fatal(err)
	}
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, database, directory
}
