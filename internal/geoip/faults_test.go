package geoip_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/testutil"
)

func TestGeoIPStorageFailuresRemainAtomic(t *testing.T) {
	cases := []struct {
		name, trigger string
		action        func(*testing.T, *fixture) error
	}{
		{"location_insert", `CREATE TRIGGER fail BEFORE INSERT ON node_locations BEGIN SELECT RAISE(ABORT,'fail');END`, saveManual},
		{"location_write", `CREATE TRIGGER fail BEFORE UPDATE ON node_locations BEGIN SELECT RAISE(ABORT,'fail');END`, saveManual},
		{"settings_write", `CREATE TRIGGER fail BEFORE UPDATE ON geoip_settings BEGIN SELECT RAISE(ABORT,'fail');END`, saveRemote},
		{"settings_schedule", `CREATE TRIGGER fail BEFORE UPDATE ON node_locations BEGIN SELECT RAISE(ABORT,'fail');END`, saveRemote},
		{"commit", `CREATE TABLE geoip_commit_fail(id TEXT REFERENCES nodes(id) DEFERRABLE INITIALLY DEFERRED);CREATE TRIGGER fail AFTER UPDATE ON node_locations BEGIN INSERT INTO geoip_commit_fail VALUES('missing');END`, saveManual},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.observe(t, "8.8.8.8")
			f.exec(t, test.trigger)
			if err := test.action(t, f); err == nil {
				t.Fatal("fault succeeded")
			}
			value, err := f.service.Location(t.Context(), f.actor, "geo-node")
			if err != nil || value.Mode != "observed" {
				t.Fatal("partial mutation", value, err)
			}
		})
	}
}

func saveManual(t *testing.T, f *fixture) error {
	t.Helper()
	_, err := f.service.SaveLocation(t.Context(), f.actor, geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "manual", Scope: "public", CountryCode: "US", Visibility: "public"}})
	return err
}

func saveRemote(t *testing.T, f *fixture) error {
	t.Helper()
	_, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1})
	return err
}

func TestGeoIPClosedDatabaseAndUnavailableSecrets(t *testing.T) {
	if _, err := geoip.NewService(geoip.Config{}); err != geoip.ErrUnavailable {
		t.Fatal(err)
	}
	f := newFixture(t)
	service, err := geoip.NewService(geoip.Config{Database: f.db.DB, Secrets: f.secrets, Directory: filepath.Join(t.TempDir(), "assets")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if err = f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Settings(t.Context(), f.actor); err == nil {
		t.Fatal("closed db settings")
	}
	if _, err = service.PublicLocation(t.Context(), "geo-node"); err == nil {
		t.Fatal("closed db public")
	}
	if err = service.Ready(t.Context()); err == nil {
		t.Fatal("closed readiness")
	}
	if err = service.Process(t.Context()); err == nil {
		t.Fatal("closed worker")
	}
}

type failureSecrets struct {
	base       secretbox.ResourceService
	seal, open bool
}

func (f failureSecrets) WithWrite(ctx context.Context, fn func(secretbox.ResourceAccess) error) error {
	return f.base.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		return fn(failureAccess{ResourceAccess: access, seal: f.seal, open: f.open})
	})
}

func (f failureSecrets) OpenResource(ctx context.Context, raw []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	return f.base.OpenResource(ctx, raw, aad)
}

type failureAccess struct {
	secretbox.ResourceAccess
	seal, open bool
}

func (f failureAccess) Seal(raw []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if f.seal {
		return nil, errors.New("PRIVATE_SECRET")
	}
	return f.ResourceAccess.Seal(raw, aad)
}

func (f failureAccess) Open(raw []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if f.open {
		return nil, errors.New("PRIVATE_SECRET")
	}
	return f.ResourceAccess.Open(raw, aad)
}

func (f failureAccess) GeoIPFingerprint(raw []byte) (string, error) {
	return f.ResourceAccess.(secretbox.GeoIPFingerprinter).GeoIPFingerprint(raw)
}

func (f failureAccess) VerifyGeoIPFingerprint(raw []byte, value string) bool {
	return f.ResourceAccess.(secretbox.GeoIPFingerprinter).VerifyGeoIPFingerprint(raw, value)
}

func TestGeoIPSecretFailuresAreSanitized(t *testing.T) {
	for _, fail := range []string{"seal", "open"} {
		t.Run(fail, func(t *testing.T) {
			f := newFixture(t)
			f.observe(t, "8.8.8.8")
			token := "PRIVATE_TOKEN"
			if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "ipinfo_https", RemoteConsent: true, Credential: &token, ExpectedRevision: 1}); err != nil {
				t.Fatal(err)
			}
			service, err := geoip.NewService(geoip.Config{Database: f.db.DB, Secrets: failureSecrets{base: f.secrets, seal: fail == "seal", open: fail == "open"}, Now: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = service.Close() })
			if fail == "seal" {
				if _, err = service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "ipinfo_https", RemoteConsent: true, Credential: &token, ExpectedRevision: 2}); err != geoip.ErrUnavailable {
					t.Fatal(err)
				}
				if _, err = service.SaveLocation(t.Context(), f.actor, geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "managed_target", ManagedTarget: "8.8.4.4", Visibility: "private", ConfirmManagedTarget: true}}); err != geoip.ErrUnavailable {
					t.Fatal(err)
				}
			} else {
				if err = service.Process(t.Context()); err != geoip.ErrUnavailable {
					t.Fatal(err)
				}
				if _, err = service.SaveLocation(t.Context(), f.actor, geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "disabled", Visibility: "private"}}); err != geoip.ErrUnavailable {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRejectedMMDBActionsDoNotConsumeInput(t *testing.T) {
	f := newFixture(t)
	input := geoip.MMDBUpload{Body: bytes.NewReader(testutil.CountryMMDB("US", 1700000000)), ExpectedRevision: 1}
	if _, err := f.service.InstallMMDB(t.Context(), geoip.Actor{}, input); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.RollbackMMDB(t.Context(), geoip.Actor{}, 1); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.RollbackMMDB(t.Context(), f.actor, 1); err == nil {
		t.Fatal("missing previous")
	}
	if _, err := f.service.UpdateMMDB(t.Context(), f.actor, geoip.MMDBUpdate{}); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	if _, err := f.service.UpdateMMDB(t.Context(), geoip.Actor{}, geoip.MMDBUpdate{AccountID: "1", LicenseKey: "key", Edition: "GeoLite2-Country"}); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
}
