package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretVersionDispatchRejectsMalformedMigrationLedger(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "snapshot.sqlite")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if handled, err := validateV9Secrets(t.Context(), path, filepath.Join(directory, "missing.key")); handled || err != nil {
		t.Fatal("pre-ledger archive did not select the typed legacy validator")
	}
	if _, err := database.Exec("CREATE TABLE schema_migrations(version TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO schema_migrations VALUES('invalid')"); err != nil {
		t.Fatal(err)
	}
	if handled, err := validateV9Secrets(t.Context(), path, filepath.Join(directory, "missing.key")); handled || err == nil {
		t.Fatal("invalid schema version entered a legacy decoder")
	}
}
