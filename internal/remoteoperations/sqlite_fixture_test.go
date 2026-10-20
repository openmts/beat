package remoteoperations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

var operationsSchemaImage = sync.OnceValues(buildOperationsSchemaImage)

func buildOperationsSchemaImage() (image []byte, resultErr error) {
	directory, err := os.MkdirTemp("", "beat-operations-schema-")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(directory)) }()
	path := filepath.Join(directory, "schema.db")
	database, err := store.NewSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	var busy, log, checkpointed int
	err = database.DB.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed)
	closeErr := database.Close()
	if err != nil || closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	if busy != 0 {
		return nil, fmt.Errorf("operations fixture checkpoint remained busy")
	}
	return os.ReadFile(path)
}

func operationsFixturePath(t *testing.T) string {
	t.Helper()
	image, err := operationsSchemaImage()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "beat.db")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSchemaFixtureDatabasesAndSecretsAreIndependent(t *testing.T) {
	firstPath, secondPath := operationsFixturePath(t), operationsFixturePath(t)
	first, err := store.NewSQLiteStore(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := store.NewSQLiteStore(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if firstPath == secondPath {
		t.Fatal("shared database path")
	}
	if _, err := first.DB.Exec("CREATE TABLE fixture_private(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='fixture_private'").Scan(&count); err != nil || count != 0 {
		t.Fatal("schema leaked", count, err)
	}
	for _, path := range []string{firstPath, secondPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("database permissions", err)
		}
		info, err = os.Stat(filepath.Dir(path))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("directory permissions", err)
		}
	}
	var journal string
	var foreignKeys int
	if err := second.DB.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatal(journal, err)
	}
	if err := second.DB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal(foreignKeys, err)
	}
	firstSecrets, secondSecrets := testutil.NewSecrets(t, first.DB), testutil.NewSecrets(t, second.DB)
	if firstSecrets == secondSecrets || !firstSecrets.Status().Healthy || !secondSecrets.Status().Healthy {
		t.Fatal("secrets not independently initialized")
	}
	aad := secretbox.OperationTaskAAD("fixture")
	var sealed []byte
	err = firstSecrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		var sealErr error
		sealed, sealErr = access.Seal([]byte("independent"), aad)
		return sealErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstSecrets.OpenResource(t.Context(), sealed, aad); err != nil {
		t.Fatal(err)
	}
	if _, err := secondSecrets.OpenResource(t.Context(), sealed, aad); err == nil {
		t.Fatal("fixtures shared encryption keys")
	}
}
