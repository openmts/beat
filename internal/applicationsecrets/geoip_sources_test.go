package applicationsecrets

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestGeoIPResourcesRotateAndValidate(t *testing.T) {
	database, directory := lifecycleTestDB(t)

	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	seedGeoIPResources(t, service)
	sources, err := geoIPSourcesForDatabase(t.Context(), database.DB)
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	fingerprint, err := service.ring.GeoIPFingerprint([]byte("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if !service.ring.VerifyGeoIPFingerprint([]byte("8.8.8.8"), fingerprint) {
		t.Fatal("old fingerprint lost")
	}
	for _, source := range sources {
		resources, err := source.readBatch(t.Context(), database.DB, "")
		if err != nil || len(resources) != 1 {
			t.Fatal(source.name, resources, err)
		}
		id, err := secretbox.EnvelopeDataKeyID(resources[0].ciphertext)
		if err != nil || id != service.ring.active {
			t.Fatal("rotation skipped", err)
		}
	}
	if err = ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
	if service.ring.VerifyGeoIPFingerprint(nil, "bad") || service.ring.VerifyGeoIPFingerprint(nil, "missing:hash") {
		t.Fatal("bad fingerprint accepted")
	}
	if _, err = (&keyring{}).GeoIPFingerprint(nil); err != ErrUnhealthy {
		t.Fatal(err)
	}
}

func seedGeoIPResources(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.db.Exec(`INSERT INTO nodes(id,name,host,port,created_at,updated_at) VALUES('geo-node','Geo Node','untrusted',22,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	source, err := service.ring.Seal([]byte("8.8.8.8"), secretbox.GeoIPSourceAAD("geo-node", "observed"))
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.ring.Seal([]byte("PRIVATE_TOKEN"), secretbox.GeoIPCredentialAAD())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.db.Exec(`INSERT INTO node_locations(id,source_encrypted,updated_at) VALUES('geo-node',?,CURRENT_TIMESTAMP)`, source); err != nil {
		t.Fatal(err)
	}
	if _, err = service.db.Exec(`UPDATE geoip_settings SET credential_encrypted=?`, credential); err != nil {
		t.Fatal(err)
	}
}

func TestGeoIPPartialSchemaFailsClosed(t *testing.T) {
	database, _ := lifecycleTestDB(t)
	if _, err := database.DB.Exec(`ALTER TABLE geoip_cache RENAME TO missing_geoip_cache`); err != nil {
		t.Fatal(err)
	}

	if _, err := geoIPSourcesForDatabase(t.Context(), database.DB); err != ErrUnhealthy {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := geoIPSourcesForDatabase(t.Context(), database.DB); err == nil {
		t.Fatal("closed schema")
	}
}

func TestGeoIPAbsentNamespaceIsNotRegistered(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	sources, err := geoIPSourcesForDatabase(t.Context(), database)
	if err != nil || len(sources) != 0 {
		t.Fatal(sources, err)
	}
}
