package applicationsecrets

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestEgressProxySecretRotatesAndSnapshotValidationDetectsSubstitution(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	sealed, err := service.ring.Seal([]byte(`{"proxy_url":"http://127.0.0.1:7890"}`), secretbox.EgressProxyAAD(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.db.Exec(`UPDATE egress_proxy_settings SET proxy_secret_encrypted=?`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	sources, err := egressProxySourcesForDatabase(t.Context(), database.DB)
	if err != nil || len(sources) != 1 {
		t.Fatal(sources, err)
	}
	resources, err := sources[0].readBatch(t.Context(), database.DB, "")
	if err != nil || len(resources) != 1 {
		t.Fatal(resources, err)
	}
	keyID, err := secretbox.EnvelopeDataKeyID(resources[0].ciphertext)
	if err != nil || keyID != service.ring.active {
		t.Fatal("rotation skipped proxy secret", err)
	}
	databasePath := filepath.Join(directory, "beat.db")
	keyPath := filepath.Join(directory, "admin-data.key")
	if err = ValidateSnapshot(t.Context(), databasePath, keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err = service.db.Exec(`UPDATE egress_proxy_settings SET revision=2`); err != nil {
		t.Fatal(err)
	}
	if err = ValidateSnapshot(t.Context(), databasePath, keyPath); err == nil {
		t.Fatal("snapshot accepted ciphertext under the wrong revision")
	}
}

func TestEgressProxySourceAbsentOnLegacyDatabase(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	sources, err := egressProxySourcesForDatabase(t.Context(), database)
	if err != nil || len(sources) != 0 {
		t.Fatal(sources, err)
	}
}
