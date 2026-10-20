package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/testutil"
)

func TestGeoIPCaptureKeepsFilesAndSQLiteInOnePublication(t *testing.T) {
	fixture := newBackupFixture(t)
	assets := configureGeoIPBackup(t, fixture)
	directory := t.TempDir()
	for _, asset := range assets {
		if err := copyFile(asset.Path, filepath.Join(directory, asset.Name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := geoip.NewService(geoip.Config{Database: fixture.sqlite.DB, Secrets: fixture.service.secretLifecycle.(secretbox.ResourceService), Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = provider.Close() }()
	entered, release := make(chan struct{}), make(chan struct{})
	fixture.service.SetGeoIPAssets(geoIPAssetProvider(func(ctx context.Context, capture func([]geoip.BackupAsset) error) error {
		return provider.WithBackupAssets(ctx, func(assets []geoip.BackupAsset) error {
			close(entered)
			<-release
			return capture(assets)
		})
	}), "")
	root := t.TempDir()
	captured := make(chan error, 1)
	go func() { _, err := fixture.service.captureArchive(t.Context(), root); captured <- err }()
	<-entered
	published := make(chan error, 1)
	go func() {
		published <- provider.WithBackupAssets(t.Context(), func([]geoip.BackupAsset) error {
			path := filepath.Join(directory, "country.mmdb")
			if err := os.WriteFile(path, testutil.CountryMMDB("JP", 1700000010), 0o600); err != nil {
				return err
			}
			metadata, err := geoip.ValidateAsset(path)
			if err != nil {
				return err
			}
			_, err = fixture.sqlite.DB.Exec(`UPDATE geoip_settings SET mmdb_sha256=?,mmdb_build_epoch=?,mmdb_size=?`, metadata.SHA256, metadata.BuildEpoch, metadata.SizeBytes)
			return err
		})
	}()
	close(release)
	if err := errors.Join(<-captured, <-published); err != nil {
		t.Fatal(err)
	}
	frozen, err := geoip.ValidateAsset(filepath.Join(root, geoIPCurrentEntry))
	if err != nil || frozen.SHA256 != assets[0].Metadata.SHA256 {
		t.Fatal("captured a mixed publication", err)
	}
	db, err := openArchivedSQLite(filepath.Join(root, sqliteEntry))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var checksum string
	if err := db.QueryRow("SELECT mmdb_sha256 FROM geoip_settings").Scan(&checksum); err != nil || checksum != frozen.SHA256 {
		t.Fatal("SQLite and MMDB disagree", err)
	}
}
