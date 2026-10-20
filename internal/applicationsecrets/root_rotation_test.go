package applicationsecrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootRotationRecoversEveryDurablePhase(t *testing.T) {
	phases := []string{"root_prepare_created", "root_old_saved", "root_next_saved", "root_journal_saved",
		"root_journal_published", "root_before_commit", "root_db_committed", "root_key_published", "root_verified", "root_cleanup_published"}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
			injected := errors.New("injected crash")
			err = rotateRoot(t.Context(), RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"),
				fault: func(current string) error {
					if current == phase {
						return injected
					}
					return nil
				}})
			if !errors.Is(err, injected) {
				t.Fatalf("phase %s: %v", phase, err)
			}
			if err := RecoverRootFiles(t.Context(), directory, filepath.Join(directory, "beat.db")); err != nil {
				t.Fatal(err)
			}
			if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
				t.Fatal(err)
			}
			for _, name := range rotationDirectories {
				if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("journal remains: %s, %v", name, err)
				}
			}
		})
	}
}

func TestRootRotationPreservesDataAndRefusesActiveLease(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	lease, err := AcquireLease(directory)
	if err != nil {
		t.Fatal(err)
	}
	config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}
	if err := rotateRoot(t.Context(), config); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("active server: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := readRootKey(filepath.Join(directory, "admin-data.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rotateRoot(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	after, err := readRootKey(filepath.Join(directory, "admin-data.key"))
	if err != nil || bytes.Equal(before, after) {
		t.Fatalf("root did not change: %v", err)
	}
	if err := ValidateSnapshot(t.Context(), config.DatabasePath, filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}
