package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 普通CRUD测试共享只读schema镜像；每次复制仍使用独立真实SQLite/WAL。
var sqliteFixtureImage = sync.OnceValues(buildSQLiteFixtureImage)

func buildSQLiteFixtureImage() (image []byte, resultErr error) {
	directory, err := os.MkdirTemp("", "beat-store-schema-")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(directory)) }()
	path := filepath.Join(directory, "schema.db")
	seed, err := NewSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	var busy, log, checkpointed int
	err = seed.DB.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed)
	closeErr := seed.Close()
	if err != nil || closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	if busy != 0 {
		return nil, fmt.Errorf("fixture checkpoint remained busy")
	}
	return os.ReadFile(path)
}

func setupTestDB(t *testing.T) *SQLiteStore {
	t.Helper()
	image, err := sqliteFixtureImage()
	if err != nil {
		t.Fatal("build complete SQLite fixture", err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fixture.db")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	instance := &SQLiteStore{DB: database, path: path}
	t.Cleanup(func() { _ = instance.Close() })
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(8)
	database.SetConnMaxIdleTime(5 * time.Minute)
	if err := enableSQLiteWAL(t.Context(), database, path); err != nil {
		t.Fatal(err)
	}
	if err := secureSQLiteFiles(path); err != nil {
		t.Fatal(err)
	}
	return instance
}

func TestSQLiteFixtureClonesAreIndependentAndRetainConnectionPolicy(t *testing.T) {
	first, second := setupTestDB(t), setupTestDB(t)
	if first.path == second.path {
		t.Fatal("fixtures shared a file")
	}
	if _, err := first.DB.Exec("CREATE TABLE fixture_private(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='fixture_private'").Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture schema leaked", count, err)
	}
	info, err := os.Stat(first.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	directoryInfo, err := os.Stat(filepath.Dir(first.path))
	if err != nil || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatal(directoryInfo, err)
	}
	connections := []*sql.Conn{}
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	for range 8 {
		connection, err := second.DB.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		assertFixtureConnection(t, connection)
	}
}

func assertFixtureConnection(t *testing.T, connection *sql.Conn) {
	t.Helper()
	var foreignKeys, version, busy int
	var journal string
	for statement, target := range map[string]any{"PRAGMA foreign_keys": &foreignKeys, "PRAGMA busy_timeout": &busy, "PRAGMA journal_mode": &journal, "SELECT MAX(version) FROM schema_migrations": &version} {
		if err := connection.QueryRowContext(context.Background(), statement).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if foreignKeys != 1 || busy != 5000 || journal != "wal" || version != currentSchemaVersion {
		t.Fatal(foreignKeys, busy, journal, version)
	}
}
