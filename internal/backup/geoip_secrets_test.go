package backup

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/secretbox"
)

func TestGeoIPBackupAuthenticatesProviderCredentialWithSnapshotKey(t *testing.T) {
	fixture := newBackupFixture(t)
	configureGeoIPBackup(t, fixture)
	secrets := fixture.service.secretLifecycle.(secretbox.ResourceService)
	expected := []byte("fixture-provider-credential")
	err := secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		sealed, err := access.Seal(expected, secretbox.GeoIPCredentialAAD())
		if err != nil {
			return err
		}
		_, err = fixture.sqlite.DB.Exec(`UPDATE geoip_settings SET credential_encrypted=? WHERE id='global'`, sealed)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, record := generatedArchive(t, fixture)
	validated, err := validateArchive(t.Context(), filepath.Join(fixture.dataDir, "backups", record.Filename), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	database := filepath.Join(validated.root, sqliteEntry)
	key := filepath.Join(validated.root, dataKeyEntry)
	if err := applicationsecrets.ValidateSnapshot(t.Context(), database, key); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var ciphertext []byte
	if err := db.QueryRow(`SELECT credential_encrypted FROM geoip_settings`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, expected) {
		t.Fatal("credential stored as plaintext")
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := db.Exec(`UPDATE geoip_settings SET credential_encrypted=?`, ciphertext); err != nil {
		t.Fatal(err)
	}
	if err := applicationsecrets.ValidateSnapshot(t.Context(), database, key); err == nil {
		t.Fatal("tampered provider credential accepted")
	}
}
