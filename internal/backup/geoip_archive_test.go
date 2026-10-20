package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

type geoIPAssetProvider func(context.Context, func([]geoip.BackupAsset) error) error

func (provider geoIPAssetProvider) WithBackupAssets(ctx context.Context, capture func([]geoip.BackupAsset) error) error {
	return provider(ctx, capture)
}

func geoIPTestAsset(t *testing.T, name string) geoip.BackupAsset {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, testutil.CountryMMDB("US", 1700000000), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := geoip.ValidateAsset(path)
	if err != nil {
		t.Fatal(err)
	}
	return geoip.BackupAsset{Name: name, Path: path, Metadata: metadata}
}

func configureGeoIPBackup(t *testing.T, fixture *backupFixture) []geoip.BackupAsset {
	t.Helper()
	var registered int
	if err := fixture.sqlite.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=13").Scan(&registered); err != nil || registered != 1 {
		t.Fatalf("backup fixture requires registered schema v13: count=%d error=%v", registered, err)
	}
	assets := []geoip.BackupAsset{geoIPTestAsset(t, "country.mmdb"), geoIPTestAsset(t, "country.mmdb.previous")}
	current := assets[0].Metadata
	if _, err := fixture.sqlite.DB.Exec(`UPDATE geoip_settings SET mmdb_source='upload',mmdb_edition=?,mmdb_sha256=?,mmdb_size=?,mmdb_build_epoch=?,mmdb_state='ready' WHERE id='global'`, current.Edition, current.SHA256, current.SizeBytes, current.BuildEpoch); err != nil {
		t.Fatal(err)
	}
	fixture.service.SetGeoIPAssets(geoIPAssetProvider(func(ctx context.Context, capture func([]geoip.BackupAsset) error) error { return capture(assets) }), "")
	return assets
}

func TestGeoIPArchiveRoundTripAndMetadataConsistency(t *testing.T) {
	fixture := newBackupFixture(t)
	assets := configureGeoIPBackup(t, fixture)
	_, record := generatedArchive(t, fixture)
	archive := filepath.Join(fixture.dataDir, "backups", record.Filename)
	validated, err := validateArchive(t.Context(), archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	if validated.manifest.FormatVersion != 4 || len(validated.manifest.GeoIPFiles) != 2 {
		t.Fatal(validated.manifest)
	}
	target := restoreNotificationArchive(t, fixture, record)
	for _, asset := range assets {
		path := filepath.Join(target, "geoip", asset.Name)
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(testutil.CountryMMDB("US", 1700000000), actual) {
			t.Fatal("restored MMDB", err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("restored permissions", err)
		}
	}
	metadata := validated.manifest.GeoIPFiles[geoIPCurrentEntry]
	metadata.BuildEpoch++
	validated.manifest.GeoIPFiles[geoIPCurrentEntry] = metadata
	if err := validateGeoIPPayloads(validated.root, validated.manifest); err == nil {
		t.Fatal("forged build epoch accepted")
	}
}

func TestGeoIPCaptureRejectsUnsafeChangedAndDuplicateAssets(t *testing.T) {
	asset := geoIPTestAsset(t, "country.mmdb")
	for _, kind := range []string{"name", "duplicate", "missing", "directory", "public", "changed", "invalid", "blocked"} {
		t.Run(kind, func(t *testing.T) {
			altered := asset
			root := t.TempDir()
			list := []geoip.BackupAsset{altered}
			switch kind {
			case "name":
				list[0].Name = "../escape"
			case "duplicate":
				list = append(list, asset)
			case "missing":
				list[0].Path = filepath.Join(root, "absent")
			case "directory":
				list[0].Path = root
			case "public":
				list[0].Path = filepath.Join(root, "public")
				if err := os.WriteFile(list[0].Path, []byte("invalid"), 0o644); err != nil {
					t.Fatal(err)
				}
				// 门禁在 umask 077 下运行，显式放开权限才能复现"公共可读"夹具。
				if err := os.Chmod(list[0].Path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "changed":
				list[0].Metadata.SHA256 = "changed"
			case "invalid":
				list[0].Path = filepath.Join(root, "invalid")
				if err := os.WriteFile(list[0].Path, []byte("invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				if err := os.WriteFile(filepath.Join(root, "geoip"), []byte("blocked"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := captureGeoIPAssets(root, list, &archiveCapture{}); err == nil {
				t.Fatal("unsafe capture succeeded")
			}
		})
	}
}

func TestGeoIPProviderErrorsAndExternalMountMetadata(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.service.SetGeoIPAssets(geoIPAssetProvider(func(context.Context, func([]geoip.BackupAsset) error) error { return errors.New("busy") }), "")
	if _, err := fixture.service.captureArchive(t.Context(), t.TempDir()); err == nil {
		t.Fatal("provider error ignored")
	}
	fixture.service.SetGeoIPAssets(nil, "/licensed/absent-country.mmdb")
	_, record := generatedArchive(t, fixture)
	validated, err := validateArchive(t.Context(), filepath.Join(fixture.dataDir, "backups", record.Filename), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	if validated.manifest.ExternalMMDB != "/licensed/absent-country.mmdb" || len(validated.manifest.GeoIPFiles) != 0 {
		t.Fatal(validated.manifest)
	}
	for _, version := range []int{1, 2, 3} {
		manifest := validated.manifest
		manifest.FormatVersion = version
		if err := validateManifestHeader(manifest); err == nil {
			t.Fatal("legacy external metadata accepted")
		}
	}
}

func TestGeoIPManifestRejectsUnknownMissingAndMixedAssets(t *testing.T) {
	for _, manifest := range []model.BackupManifest{
		{FormatVersion: 3, GeoIPFiles: map[string]model.MMDBMetadata{geoIPCurrentEntry: {}}},
		{FormatVersion: 4, GeoIPFiles: map[string]model.MMDBMetadata{"geoip/.publication.json": {}}},
		{FormatVersion: 4, GeoIPFiles: map[string]model.MMDBMetadata{geoIPPreviousEntry: {}}},
		{FormatVersion: 4, ExternalMMDB: "relative.mmdb"},
		{FormatVersion: 4, ExternalMMDB: "/external", GeoIPFiles: map[string]model.MMDBMetadata{geoIPCurrentEntry: {}}},
	} {
		if err := validateGeoIPManifest(manifest); err == nil {
			t.Fatal("invalid manifest accepted", manifest)
		}
	}
}
