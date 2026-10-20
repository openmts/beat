package applicationsecrets

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

func TestDataRotationExcludesBackupAndResumes(t *testing.T) {
	service, database, directory := legacyTOTPFixture(t)
	release, err := service.AcquireBackup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(t.Context()); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("rotation during backup: %v", err)
	}
	release()
	release()
	service.fault = func(phase string) error {
		if phase == "rotation_activated" {
			return errors.New("injected interruption")
		}
		return nil
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if _, err := service.AcquireBackup(t.Context()); err == nil {
		t.Fatal("backup accepted degraded rotation")
	}
	service.Close()
	resumed, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resumed.Close)
	if got := resumed.Status(); !got.Healthy || got.Generation != 2 || got.DataKeys != 2 || got.Rotation != "idle" {
		t.Fatalf("resumed status = %#v", got)
	}
	var ciphertext []byte
	if err := database.DB.QueryRow("SELECT totp_secret_encrypted FROM admin_users WHERE id = 'user-a'").Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	id, err := secretbox.EnvelopeDataKeyID(ciphertext)
	if err != nil || id != resumed.ring.active {
		t.Fatalf("resource did not migrate: %v", err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func TestDataRotationRetentionBoundAndRetirement(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	now := time.Now().UTC()
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	for index := 1; index < maximumDataKeys; index++ {
		if _, err := service.StartRotation(t.Context()); err != nil {
			t.Fatal(err)
		}
		service.wg.Wait()
	}
	if _, err := service.StartRotation(t.Context()); !errors.Is(err, ErrKeyLimit) {
		t.Fatalf("key limit = %v", err)
	}
	now = now.Add(keyRetention + time.Second)
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if service.Status().DataKeys != 2 {
		t.Fatalf("expired keys not retired: %#v", service.Status())
	}
}
