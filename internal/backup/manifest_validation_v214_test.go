package backup

import (
	"testing"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestReadManifestRejectsUnboundedStream(t *testing.T) {
	if _, err := readManifest("/dev/zero"); err == nil {
		t.Fatal("unbounded manifest stream accepted")
	}
}

func TestValidateManifestVersionRejectsUnsupportedShapes(t *testing.T) {
	cases := map[string]struct {
		manifest model.BackupManifest
		wantErr  bool
	}{
		"format one":                     {manifest: model.BackupManifest{FormatVersion: 1}},
		"format two without theme files": {manifest: model.BackupManifest{FormatVersion: 2}, wantErr: true},
		"unsupported format":             {manifest: model.BackupManifest{FormatVersion: 9}, wantErr: true},
		"catalog version mismatch": {
			manifest: model.BackupManifest{FormatVersion: 3, CatalogVersion: metriccatalog.Version + 1, CatalogRevision: 1},
			wantErr:  true,
		},
		"catalog revision missing": {
			manifest: model.BackupManifest{FormatVersion: 4, CatalogVersion: metriccatalog.Version},
			wantErr:  true,
		},
		"geoip metadata needs format four": {
			manifest: model.BackupManifest{FormatVersion: 3, CatalogVersion: metriccatalog.Version, CatalogRevision: 1,
				GeoIPFiles: map[string]model.MMDBMetadata{geoIPCurrentEntry: {Edition: "test"}}},
			wantErr: true,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateManifestVersion(testCase.manifest)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validateManifestVersion error = %v", err)
			}
		})
	}
	compatible := model.BackupManifest{FormatVersion: 3, CatalogVersion: metriccatalog.Version, CatalogRevision: 1}
	if err := validateManifestVersion(compatible); err != nil {
		t.Fatalf("compatible manifest rejected: %v", err)
	}
}
