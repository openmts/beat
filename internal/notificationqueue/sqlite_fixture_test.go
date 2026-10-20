package notificationqueue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/beat/backend/internal/store"
)

var queueSchemaImage = sync.OnceValues(buildQueueSchemaImage)

func buildQueueSchemaImage() (image []byte, resultErr error) {
	directory, err := os.MkdirTemp("", "beat-queue-schema-")
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
		return nil, fmt.Errorf("queue fixture checkpoint remained busy")
	}
	return os.ReadFile(path)
}

func queueFixturePath(t *testing.T) string {
	t.Helper()
	image, err := queueSchemaImage()
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

func TestQueueFixtureDatabasesAndSecretsRemainIndependent(t *testing.T) {
	first, firstDB := queueFixture(t)
	second, secondDB := queueFixture(t)
	if first.db == second.db || first.secrets == second.secrets {
		t.Fatal("fixture shared mutable service")
	}
	if _, err := firstDB.DB.Exec("CREATE TABLE fixture_marker(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := secondDB.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='fixture_marker'").Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture schema leaked", err)
	}
	var mode string
	if err := secondDB.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal("fixture lost WAL", err)
	}
	var foreignKeys int
	if err := secondDB.DB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("fixture lost foreign keys", err)
	}
}
