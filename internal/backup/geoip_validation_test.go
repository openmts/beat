package backup

import (
	"archive/zip"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

func TestGeoIPSnapshotRejectsMismatchMissingAndLegacyMetadata(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, sqliteEntry))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	manifest := model.BackupManifest{FormatVersion: 4, GeoIPFiles: map[string]model.MMDBMetadata{geoIPCurrentEntry: {SHA256: "expected", Edition: "GeoLite2-Country", SizeBytes: 10, BuildEpoch: 5}}}
	if err := validateGeoIPSnapshot(root, manifest); err == nil {
		t.Fatal("missing GeoIP schema accepted")
	}
	if _, err := db.Exec(`CREATE TABLE geoip_settings(id TEXT,mmdb_edition TEXT,mmdb_sha256 TEXT,mmdb_size INTEGER,mmdb_build_epoch INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := validateGeoIPSnapshot(root, manifest); err == nil {
		t.Fatal("missing settings accepted")
	}
	if _, err := db.Exec(`INSERT INTO geoip_settings VALUES('global','GeoLite2-Country','',10,5)`); err != nil {
		t.Fatal(err)
	}
	if err := validateGeoIPSnapshot(root, manifest); err == nil {
		t.Fatal("unregistered asset accepted")
	}
	if _, err := db.Exec(`UPDATE geoip_settings SET mmdb_sha256='different'`); err != nil {
		t.Fatal(err)
	}
	if err := validateGeoIPSnapshot(root, manifest); err == nil {
		t.Fatal("snapshot mismatch accepted")
	}
	manifest.FormatVersion = 3
	if err := validateGeoIPSnapshot(root, manifest); err == nil {
		t.Fatal("legacy managed asset accepted")
	}
	manifest.ExternalMMDB = "/external/country.mmdb"
	if err := validateGeoIPSnapshot(root, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestGeoIPPayloadRejectsUndeclaredUnsafeCorruptAndOversizedFiles(t *testing.T) {
	asset := geoIPTestAsset(t, "country.mmdb")
	for _, kind := range []string{"undeclared", "unsafe", "corrupt", "copy_exists", "inspection"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "geoip"), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, geoIPCurrentEntry)
			if err := copyFile(asset.Path, path, 0o600); err != nil {
				t.Fatal(err)
			}
			manifest := model.BackupManifest{FormatVersion: 4, GeoIPFiles: map[string]model.MMDBMetadata{geoIPCurrentEntry: asset.Metadata}, Checksums: map[string]string{geoIPCurrentEntry: asset.Metadata.SHA256}, PayloadSizes: map[string]int64{geoIPCurrentEntry: asset.Metadata.SizeBytes}}
			switch kind {
			case "undeclared":
				delete(manifest.GeoIPFiles, geoIPCurrentEntry)
			case "unsafe":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "copy_exists":
				if _, err := copyGeoIPAsset(root, asset); err == nil {
					t.Fatal("existing target overwritten")
				}
				return
			case "inspection":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("geoip", filepath.Join(root, "geoip")); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateGeoIPPayload(root, geoIPCurrentEntry, manifest); err == nil {
				t.Fatal("invalid payload accepted")
			}
		})
	}
	files := fakeZipFiles(zip.FileHeader{Name: geoIPCurrentEntry, UncompressedSize64: geoip.MaximumMMDBBytes + 1})
	if err := extractAllowlisted(files, t.TempDir()); err == nil {
		t.Fatal("oversized MMDB entry accepted")
	}
	if err := validateGeoIPPayloads(t.TempDir(), model.BackupManifest{ExternalMMDB: "relative"}); err == nil {
		t.Fatal("invalid overall manifest accepted")
	}
}
