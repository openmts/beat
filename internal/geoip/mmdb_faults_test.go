package geoip

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/beat/backend/internal/testutil"
)

func TestMMDBCorruptionAndAssetPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := validateDatabase(dir); err != ErrInvalid {
		t.Fatal(err)
	}
	for _, content := range [][]byte{nil, testutil.CountryMMDB("ZZ", 1700000000), append([]byte{255, 255, 255}, testutil.CountryMMDB("US", 1700000000)[3:]...)} {
		path := filepath.Join(dir, "invalid.mmdb")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := validateDatabase(path); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	s := publicationFixture(t)
	putDatabase(t, s.assetPath("country.mmdb"), "US")
	if err := os.Chmod(s.assetPath("country.mmdb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BackupAssets(context.Background()); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := s.WithBackupAssets(context.Background(), func([]BackupAsset) error { return nil }); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := os.Chmod(s.directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.stageDatabase(bytes.NewReader(nil)); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := removeTemporary(s.directory); err == nil {
		t.Fatal("removed nonempty directory")
	}
}

func TestMMDBLookupMissingMalformedAndBadCountry(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt", "country"} {
		t.Run(kind, func(t *testing.T) {
			content := testutil.CountryMMDB("ZZ", 1700000000)
			switch kind {
			case "missing":
				copy(content[:6], []byte{0, 0, 1, 0, 0, 1})
			case "corrupt":
				copy(content[:3], []byte{255, 255, 255})
			}
			reader, err := maxminddb.OpenBytes(content)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			value, err := lookupCountry(reader, netip.MustParseAddr("8.8.8.8"))
			if kind == "missing" {
				if err != nil || value != "" {
					t.Fatal(value, err)
				}
			} else if err != ErrUnavailable {
				t.Fatal(err)
			}
		})
	}
}

func TestVendorFailuresNeverPublish(t *testing.T) {
	for _, kind := range []string{"network", "status", "oversize_checksum", "invalid_checksum", "wrong_digest", "archive_failure"} {
		t.Run(kind, func(t *testing.T) {
			s := publicationFixture(t)
			s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if kind == "network" {
					return nil, ErrUnavailable
				}
				status := 200
				body := strings.Repeat("a", 64)
				if kind == "status" {
					status = 503
				}
				if kind == "oversize_checksum" {
					body = strings.Repeat("a", 4097)
				}
				if kind == "invalid_checksum" {
					body = "zzzz"
				}
				if !strings.HasSuffix(r.URL.RawQuery, "sha256") {
					body = "bad archive"
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			if _, err := s.downloadDatabase(t.Context(), MMDBUpdate{AccountID: "1", LicenseKey: "key", Edition: "GeoLite2-Country"}); err == nil {
				t.Fatal("vendor failure accepted")
			}
		})
	}
}

func TestArchiveIOFailures(t *testing.T) {
	directory := t.TempDir()
	if _, err := extractDatabase(filepath.Join(directory, "missing"), directory); err == nil {
		t.Fatal("missing archive")
	}
	path := filepath.Join(directory, "bad.tar.gz")
	if err := os.WriteFile(path, []byte("bad gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractDatabase(path, directory); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := writeArchiveDatabase(filepath.Join(directory, "missing"), bytes.NewReader(nil), 0); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := writeArchiveDatabase(directory, errorReader{}, 5); err != ErrInvalid {
		t.Fatal(err)
	}
}
