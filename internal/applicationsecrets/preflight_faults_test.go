package applicationsecrets

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestServicePreflightRejectsMissingAndUnsafeDependencies(t *testing.T) {
	if _, err := Open(t.Context(), Config{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	database, directory := lifecycleTestDB(t)
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: filepath.Join(directory, "missing")}); err == nil {
		t.Fatal("missing directory accepted")
	}
	keyPath := filepath.Join(directory, "admin-data.key")
	writeRootFault(t, keyPath, []byte("short"))
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		t.Fatal("invalid root accepted")
	}
	writeRootFault(t, keyPath, bytes.Repeat([]byte{5}, rootKeyBytes))
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if err := service.bootstrap(t.Context()); err == nil {
		t.Fatal("initialized registry bootstrapped twice")
	}
	if err := service.allowNewRoot(t.Context()); err == nil {
		t.Fatal("existing registry allowed replacement root")
	}
	if _, err := service.ring.Open(nil, secretbox.TOTPSecretAAD("id")); err == nil {
		t.Fatal("missing envelope accepted")
	}
	service.Close()
	if service.Status().Healthy {
		t.Fatal("closed service reported healthy")
	}
	if _, err := service.StartRotation(t.Context()); err == nil {
		t.Fatal("closed service rotated keys")
	}
}

func TestRootRotationRejectsInvalidPreflightAndEqualRoot(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	root, err := readRootKey(filepath.Join(directory, "admin-data.key"))
	if err != nil {
		t.Fatal(err)
	}
	config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"), Random: bytes.NewReader(root)}
	if err := rotateRoot(t.Context(), config); err == nil {
		t.Fatal("identical root accepted")
	}
	if err := RecoverRootFiles(t.Context(), directory, config.DatabasePath); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("UPDATE application_secret_state SET root_generation=?", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := rotateRoot(t.Context(), config); err == nil {
		t.Fatal("root generation overflow accepted")
	}
	if _, err := database.DB.Exec("UPDATE application_secret_state SET phase='rotating'"); err != nil {
		t.Fatal(err)
	}
	if err := rotateRoot(t.Context(), config); err == nil {
		t.Fatal("incomplete data rotation accepted")
	}
	if err := rotateRoot(t.Context(), RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "missing.db")}); err == nil {
		t.Fatal("missing database created")
	}
	if os.Geteuid() != 0 {
		if err := RotateRoot(t.Context(), config); err == nil {
			t.Fatal("unprivileged root rotation allowed")
		}
	}
}

func TestRecoveryAndStartupRefuseCorruptMarkers(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	if err := os.Mkdir(filepath.Join(directory, rotationJournalDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		t.Fatal("invalid journal allowed startup")
	}
	if err := RecoverRootFiles(t.Context(), directory, filepath.Join(directory, "missing.db")); err == nil {
		t.Fatal("recovery created database")
	}
	if err := removeRotationFile(filepath.Join(directory, "missing"), "old.key"); err == nil {
		t.Fatal("missing artifact parent accepted")
	}
	writeRootFault(t, filepath.Join(directory, rotationJournalDir, "unexpected"), nil)
	if err := removeRotationDirectory(filepath.Join(directory, rotationJournalDir)); err == nil {
		t.Fatal("unknown evidence discarded")
	}
}

func TestRegistryReloadAndSnapshotRejectCorruption(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if _, err := database.DB.Exec("UPDATE application_data_keys SET generation='invalid'"); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegistry(t.Context(), database.DB); err == nil {
		t.Fatal("untyped key generation accepted")
	}
	if err := service.reload(t.Context()); err == nil {
		t.Fatal("invalid registry reloaded")
	}
	if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		t.Fatal("invalid registry served")
	}
	if _, err := loadKeyring(t.Context(), database.DB, keyringConfig{root: service.root, state: service.state}); err == nil {
		t.Fatal("invalid keyring accepted")
	}
}
