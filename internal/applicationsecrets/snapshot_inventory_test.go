package applicationsecrets

import (
	"path/filepath"
	"testing"
)

func TestSnapshotInventoryDoesNotSkipEmptyOrNullResourceIDs(t *testing.T) {
	for _, identifier := range []any{"", nil} {
		t.Run(identifierName(identifier), func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.Close)
			if _, err := database.DB.Exec(`INSERT INTO ssh_keys(id,name,key_type,public_key,private_key_encrypted,created_at) VALUES(?,'bad','ed25519','public',X'00',CURRENT_TIMESTAMP)`, identifier); err != nil {
				t.Fatal(err)
			}
			if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err == nil {
				t.Fatal("ciphertext row was skipped by cursor inventory")
			}
		})
	}
}

func identifierName(value any) string {
	if value == nil {
		return "null"
	}
	return "empty"
}
