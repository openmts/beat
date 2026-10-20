package applicationsecrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMissingRootNeverCreatesReplacementForSSHOrPendingRestore(t *testing.T) {
	for _, scenario := range []string{"encrypted_ssh", "pending_restore"} {
		t.Run(scenario, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			if scenario == "encrypted_ssh" {
				if _, err := database.DB.Exec(`INSERT INTO ssh_keys(id,name,key_type,public_key,private_key,private_key_encrypted,created_at)
				VALUES('ssh','ssh','ed25519','public','',X'01',CURRENT_TIMESTAMP)`); err != nil {
					t.Fatal(err)
				}
			} else {
				writeRootFault(t, filepath.Join(directory, "restore.pending.json"), []byte(`{"state":"pending"}`))
			}
			if service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
				service.Close()
				t.Fatal("missing root with retained state was accepted")
			}
			if _, err := os.Lstat(filepath.Join(directory, "admin-data.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing root was silently replaced")
			}
		})
	}
}
