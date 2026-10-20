package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveRejectsMissingKeyWhenOIDCSecretExists(t *testing.T) {
	sqlite, secrets, key := setupOIDCBackupDB(t)
	createBackupProvider(t, sqlite, secrets, key, false)
	missing := filepath.Join(t.TempDir(), "admin-data.key")
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), missing); err == nil {
		t.Fatal("encrypted provider data was accepted without its key")
	}
}

func TestArchiveAuthenticatesTOTPSecrets(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "wrong", "tampered"} {
		t.Run(scenario, func(t *testing.T) {
			sqlite, secrets, key := setupOIDCBackupDB(t)
			ciphertext, err := secrets.Encrypt([]byte("JBSWY3DPEHPK3PXP"))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "tampered" {
				ciphertext[len(ciphertext)-1] ^= 1
			}
			_, err = sqlite.DB.Exec(`INSERT INTO admin_users(id,username,password_hash,display_name,role,enabled,
				totp_secret_encrypted,totp_enabled_at,password_changed_at,created_at,updated_at)
				VALUES('totp-owner','owner','hash','Owner','owner',1,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, ciphertext)
			if err != nil {
				t.Fatal(err)
			}
			keyPath := filepath.Join(t.TempDir(), "key")
			if scenario != "missing" {
				if scenario == "wrong" {
					key = bytes.Repeat([]byte{8}, len(key))
				}
				if err := os.WriteFile(keyPath, key, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err = validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("secret recovery validation: %v", err)
			}
		})
	}
}
