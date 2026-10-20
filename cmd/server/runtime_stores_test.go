package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/store"
)

func TestOpenServerStoresPublishesMigrationMarker(t *testing.T) {
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "beat.db")
	stores, err := openServerStores(context.Background(), dbPath, filepath.Join(directory, "mts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stores.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stores.mts.Close(); err != nil {
		t.Fatal(err)
	}
	marker, err := migrationstatus.Read(directory)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State != migrationstatus.StateCompleted || marker.CurrentSchema != store.MaximumSupportedSchemaVersion ||
		marker.CurrentMigration != 0 || marker.FailureCode != "" {
		t.Fatalf("marker: %+v", marker)
	}
}

func TestOpenServerStoresReportsFutureSchemaFailure(t *testing.T) {
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "beat.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL);
		INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'future', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openServerStores(context.Background(), dbPath, filepath.Join(directory, "mts")); err == nil {
		t.Fatal("future schema accepted")
	}
	marker, err := migrationstatus.Read(directory)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State != migrationstatus.StateFailed || marker.FailureCode != store.MigrationFailureFutureSchema || !marker.RollbackRequired {
		t.Fatalf("marker: %+v", marker)
	}
}
