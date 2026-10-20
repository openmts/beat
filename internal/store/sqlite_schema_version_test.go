package store

import "testing"

func TestSchemaVersionAndMigrationName(t *testing.T) {
	store := setupTestDB(t)
	version, err := store.SchemaVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != MaximumSupportedSchemaVersion {
		t.Fatalf("version: %d", version)
	}
	if name := MigrationName(version); name == "" {
		t.Fatal("current migration name is missing")
	}
	if MigrationName(0) != "" || MigrationName(MaximumSupportedSchemaVersion+1) != "" {
		t.Fatal("out-of-range versions must be empty")
	}
}
