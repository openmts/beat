package geoip

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/testutil"
)

func publicationFixture(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`CREATE TABLE geoip_settings(id TEXT,mmdb_sha256 TEXT);INSERT INTO geoip_settings VALUES('global','')`); err != nil {
		t.Fatal(err)
	}
	return &Service{directory: directory, db: db, mmdb: &Database{}}
}

func putDatabase(t *testing.T, path, country string) {
	t.Helper()
	if err := os.WriteFile(path, testutil.CountryMMDB(country, 1700000000), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationCrashRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "committed"}[committed], func(t *testing.T) {
			s := publicationFixture(t)
			putDatabase(t, s.assetPath("country.mmdb"), "US")
			putDatabase(t, s.assetPath("country.mmdb.previous"), "FR")
			putDatabase(t, s.assetPath("staged"), "JP")
			_, err := s.publishDatabase(s.assetPath("staged"))
			if err != nil {
				t.Fatal(err)
			}
			current, err := ValidateAsset(s.assetPath("country.mmdb"))
			if err != nil {
				t.Fatal(err)
			}
			if committed {
				if _, err = s.db.Exec(`UPDATE geoip_settings SET mmdb_sha256=?`, current.SHA256); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.RecoverPublication(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(s.assetPath("country.mmdb"))
			if err != nil {
				t.Fatal(err)
			}
			want := "US"
			if committed {
				want = "JP"
			}
			if !bytes.Equal(raw, testutil.CountryMMDB(want, 1700000000)) {
				t.Fatal("wrong recovered current")
			}
			if _, err = os.Stat(s.assetPath(".publication.json")); !os.IsNotExist(err) {
				t.Fatal("journal retained", err)
			}
		})
	}
}

func TestPublicationMissingCurrentAndInvalidJournal(t *testing.T) {
	s := publicationFixture(t)
	putDatabase(t, s.assetPath("staged"), "US")
	rollback, err := s.publishDatabase(s.assetPath("staged"))
	if err != nil {
		t.Fatal(err)
	}
	if err = rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.assetPath("country.mmdb")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err = os.WriteFile(s.assetPath(".publication.json"), []byte(`broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverPublication(); err != ErrUnavailable {
		t.Fatal(err)
	}
	journal := publicationJournal{NewChecksum: string(bytes.Repeat([]byte{'a'}, 64)), OldChecksum: string(bytes.Repeat([]byte{'b'}, 64))}
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(s.assetPath(".publication.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverPublication(); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestAssetFailuresAndExternal(t *testing.T) {
	s := publicationFixture(t)
	if _, err := ValidateAsset(s.assetPath("missing")); err == nil {
		t.Fatal("missing accepted")
	}
	if err := privateDirectory(""); err != ErrUnavailable {
		t.Fatal(err)
	}
	file := s.assetPath("file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := privateDirectory(file); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := s.stageDatabase(errorReader{}); err != ErrInvalid {
		t.Fatal(err)
	}
	if err := syncDirectory(s.assetPath("missing")); err == nil {
		t.Fatal("sync accepted missing")
	}
	s.external = "/external/Country.mmdb"
	if assets, err := s.BackupAssets(context.Background()); err != nil || len(assets) != 0 {
		t.Fatal(assets, err)
	}
	if err := s.RecoverPublication(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallMMDB(context.Background(), Actor{}, MMDBUpload{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := s.RollbackMMDB(context.Background(), Actor{}, 1); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := s.UpdateMMDB(context.Background(), Actor{}, MMDBUpdate{AccountID: "1", LicenseKey: "key", Edition: "GeoIP2-Country"}); err != ErrForbidden {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, ErrUnavailable }
