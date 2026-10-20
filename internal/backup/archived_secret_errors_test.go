package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveSecretValidationRejectsUnreadableKeyAndDatabase(t *testing.T) {
	sqlite, _, key := setupOIDCBackupDB(t)
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), t.TempDir()); err == nil {
		t.Fatal("directory key accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validateOIDCCiphertexts(canceled, sqlitePath(t, sqlite), writeBackupKey(t, key)); err == nil {
		t.Fatal("canceled secret validation succeeded")
	}
	if err := validateOIDCCiphertexts(t.Context(), filepath.Join(t.TempDir(), "missing.db"), writeBackupKey(t, key)); err == nil {
		t.Fatal("missing secret database accepted")
	}
}

func TestArchiveRejectsEnabledTOTPWithoutCiphertext(t *testing.T) {
	sqlite, _, key := setupOIDCBackupDB(t)
	_, err := sqlite.DB.Exec(`INSERT INTO admin_users(id,username,password_hash,display_name,role,enabled,totp_enabled_at,password_changed_at,created_at,updated_at)
        VALUES('owner','owner','hash','Owner','owner',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), writeBackupKey(t, key)); err == nil {
		t.Fatal("enabled MFA without a factor accepted")
	}
}

func TestArchiveRejectsMalformedEncryptedRow(t *testing.T) {
	sqlite, _, key := setupOIDCBackupDB(t)
	for _, statement := range []string{"DROP TABLE admin_users", "CREATE TABLE admin_users(totp_secret_encrypted BLOB,totp_enabled_at DATETIME)", "INSERT INTO admin_users VALUES(X'01','not-a-timestamp')"} {
		if _, err := sqlite.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), writeBackupKey(t, key)); err == nil {
		t.Fatal("malformed factor record accepted")
	}
}

func TestSQLiteSnapshotUsesLiteralPathAndValidatesTables(t *testing.T) {
	fixture := newBackupFixture(t)
	path := filepath.Join(t.TempDir(), "snapshot?mode=rw&x.db")
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLite(t.Context(), path); err != nil {
		t.Fatalf("literal snapshot path: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLite(t.Context(), path); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if _, err := fixture.sqlite.DB.Exec("DROP TABLE admin_audit_events"); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLite(t.Context(), filepath.Join(fixture.dataDir, "beat.db")); err == nil {
		t.Fatal("missing platform table accepted")
	}
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validatePlatformTables(t.Context(), fixture.sqlite.DB); err == nil {
		t.Fatal("closed table validation succeeded")
	}
}
