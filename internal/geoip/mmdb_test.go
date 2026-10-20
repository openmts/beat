package geoip_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func TestMMDBInstallRollbackAssetsAndRecovery(t *testing.T) {
	f := newFixture(t)
	first := testutil.CountryMMDB("US", 1700000000)
	settings, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(first), ExpectedRevision: 1})
	if err != nil || settings.MMDB.State != "ready" {
		t.Fatal(settings, err)
	}
	if err = f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	settings, err = f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(testutil.CountryMMDB("JP", 1700000001)), ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	assets, err := f.service.BackupAssets(t.Context())
	if err != nil || len(assets) != 2 {
		t.Fatal(assets, err)
	}
	for _, asset := range assets {
		info, err := os.Stat(asset.Path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("permission", err)
		}
	}
	settings, err = f.service.RollbackMMDB(t.Context(), f.actor, 3)
	expected := sha256.Sum256(first)
	if err != nil || settings.MMDB.SHA256 != hex.EncodeToString(expected[:]) {
		t.Fatal(settings, err)
	}
	if err = f.service.LoadDatabase(); err != nil {
		t.Fatal(err)
	}
	if err = f.service.RecoverPublication(); err != nil {
		t.Fatal(err)
	}
	if err = f.service.WithBackupAssets(t.Context(), func(assets []geoip.BackupAsset) error {
		if len(assets) != 2 {
			t.Fatal(assets)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = f.service.BackupAssets(ctx); err == nil {
		t.Fatal("canceled assets")
	}
}

func TestMMDBInvalidAndFailedCommitRetainCurrent(t *testing.T) {
	f := newFixture(t)
	if err := f.service.LoadDatabase(); err == nil {
		t.Fatal("missing database loaded")
	}
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "mmdb", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Ready(t.Context()); err != geoip.ErrUnavailable {
		t.Fatal(err)
	}
	valid := testutil.CountryMMDB("US", 1700000000)
	if _, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(valid), ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{[]byte("broken"), bytes.ReplaceAll(valid, []byte("GeoLite2-Country"), []byte("GeoLite2-City___"))} {
		if _, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(content), ExpectedRevision: 3}); err != geoip.ErrInvalid {
			t.Fatal(err)
		}
	}
	if _, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(valid), ExpectedRevision: 1}); err != geoip.ErrConflict {
		t.Fatal(err)
	}
	f.exec(t, `CREATE TRIGGER fail_mmdb BEFORE UPDATE ON geoip_settings BEGIN SELECT RAISE(ABORT,'fail');END`)
	if _, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(testutil.CountryMMDB("JP", 1700000001)), ExpectedRevision: 3}); err == nil {
		t.Fatal("write failure committed")
	}
	assets, err := f.service.BackupAssets(t.Context())
	if err != nil || len(assets) != 1 {
		t.Fatal(assets, err)
	}
	if err = f.service.LoadDatabase(); err != nil {
		t.Fatal(err)
	}
}

type memoryTransport func(*http.Request) (*http.Response, error)

func (f memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfficialUpdateUsesFixedOriginAndChecksum(t *testing.T) {
	f := newFixture(t)
	archive := countryArchive(t, testutil.CountryMMDB("GB", 1700000000))
	digest := sha256.Sum256(archive)
	requests := 0
	geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		user, password, ok := r.BasicAuth()
		if !ok || user != "123" || password != "LICENSE" || r.URL.Host != "download.maxmind.com" || r.URL.Scheme != "https" {
			t.Fatal("vendor request leaked or wrong origin")
		}
		body := archive
		if strings.HasSuffix(r.URL.RawQuery, "sha256") {
			body = []byte(hex.EncodeToString(digest[:]) + "  database.tar.gz")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})})
	settings, err := f.service.UpdateMMDB(t.Context(), f.actor, geoip.MMDBUpdate{AccountID: "123", LicenseKey: "LICENSE", Edition: "GeoLite2-Country", ExpectedRevision: 1})
	if err != nil || settings.MMDB.Source != "official" || requests != 2 {
		t.Fatal(settings, err, requests)
	}
}
