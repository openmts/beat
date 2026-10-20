package geoip_test

import (
	"bytes"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

func TestSettingsConsentCASAndCredentials(t *testing.T) {
	f := newFixture(t)
	input := model.GeoIPSettingsWrite{Provider: "geojs_https", ExpectedRevision: 1}
	if _, err := f.service.SaveSettings(t.Context(), f.actor, input); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	input.RemoteConsent = true
	settings, err := f.service.SaveSettings(t.Context(), f.actor, input)
	if err != nil || settings.Revision != 2 || !settings.RemoteConsent {
		t.Fatal(settings, err)
	}
	if _, err = f.service.SaveSettings(t.Context(), f.actor, input); err != geoip.ErrConflict {
		t.Fatal(err)
	}
	token := "PRIVATE_TOKEN"
	input = model.GeoIPSettingsWrite{Provider: "ip_api_pro_https", RemoteConsent: true, Credential: &token, ExpectedRevision: 2}
	settings, err = f.service.SaveSettings(t.Context(), f.actor, input)
	if err != nil || !settings.HasCredential {
		t.Fatal(settings, err)
	}
	var ciphertext []byte
	if err = f.db.DB.QueryRow(`SELECT credential_encrypted FROM geoip_settings`).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte(token)) {
		t.Fatal("plaintext credential", err)
	}
	input.Credential = nil
	input.ExpectedRevision = 3
	if _, err = f.service.SaveSettings(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	empty := ""
	input.Credential = &empty
	input.ExpectedRevision = 4
	if _, err = f.service.SaveSettings(t.Context(), f.actor, input); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	f.observe(t, "8.8.8.8")
	settings, err = f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "disabled", ExpectedRevision: 4, Purge: true})
	if err != nil || settings.HasCredential || settings.RemoteConsent {
		t.Fatal(settings, err)
	}
	location, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.HasSource {
		t.Fatal(location, err)
	}
}

func TestCurrentAuthorizationAndInputFailures(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Settings(t.Context(), geoip.Actor{}); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_users SET enabled=0 WHERE id=?`, f.actor.UserID)
	if _, err := f.service.Settings(t.Context(), f.actor); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_users SET role='admin',enabled=1 WHERE id=?`, f.actor.UserID)
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "disabled", ExpectedRevision: 1}); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_users SET role='owner' WHERE id=?`, f.actor.UserID)
	f.exec(t, `UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id=?`, f.actor.SessionID)
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "disabled", ExpectedRevision: 1}); err != geoip.ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.Location(t.Context(), f.actor, "missing"); err != geoip.ErrNotFound {
		t.Fatal(err)
	}
	if err := f.service.Refresh(t.Context(), f.actor, "missing"); err != geoip.ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "http"}); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	for _, input := range []model.LocationWrite{{Mode: "bad", Visibility: "inherit"}, {Mode: "manual", Scope: "public", CountryCode: "ZZ", Visibility: "public"}, {Mode: "manual", Scope: "private", CountryCode: "US", Visibility: "public"}, {Mode: "manual", Scope: "public", CountryCode: "US", Visibility: "invalid"}, {Mode: "managed_target", ManagedTarget: "host.example", Visibility: "private"}} {
		if _, err := f.service.SaveLocation(t.Context(), f.actor, geoip.LocationMutation{NodeID: "geo-node", LocationWrite: input}); err != geoip.ErrInvalid {
			t.Fatal(input, err)
		}
	}
}

func TestLocationRetainRefreshAndAuditRollback(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	input := geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "observed", Visibility: "public"}}
	if _, err := f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Refresh(t.Context(), f.actor, "geo-node"); err != nil {
		t.Fatal(err)
	}
	input.Mode = "disabled"
	location, err := f.service.SaveLocation(t.Context(), f.actor, input)
	if err != nil || !location.HasSource {
		t.Fatal(location, err)
	}
	input.Mode = "managed_target"
	input.ManagedTarget = "8.8.4.4"
	input.ConfirmManagedTarget = true
	location, err = f.service.SaveLocation(t.Context(), f.actor, input)
	if err != nil || !location.HasSource {
		t.Fatal(location, err)
	}
	f.exec(t, `CREATE TRIGGER fail_geoip_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'fail');END`)
	input.Mode = "manual"
	input.Scope = "public"
	input.CountryCode = "FR"
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err == nil {
		t.Fatal("audit failure committed")
	}
	location, err = f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.Mode != "managed_target" {
		t.Fatal(location, err)
	}
}

func TestGeoIPDuplicateInvalidSourceAndMissingNode(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	f.observe(t, "8.8.8.8")
	f.observe(t, "invalid source")
	value, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || !value.HasSource {
		t.Fatal(value, err)
	}
	input := geoip.LocationMutation{NodeID: "missing", LocationWrite: model.LocationWrite{Mode: "manual", Scope: "public", CountryCode: "US", Visibility: "public"}}
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err != geoip.ErrNotFound {
		t.Fatal(err)
	}
	token := "invalid\nsecret"
	if _, err = f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "ipinfo_https", RemoteConsent: true, Credential: &token}); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE nodes SET is_public=0 WHERE id='geo-node'`)
	if value, err := f.service.PublicLocation(t.Context(), "geo-node"); err != nil || value != nil {
		t.Fatal(value, err)
	}
}

func TestManagedTargetVisibilityEditRetainsEncryptedSource(t *testing.T) {
	f := newFixture(t)
	input := geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "managed_target", ManagedTarget: "10.0.0.1", Visibility: "private", ConfirmManagedTarget: true}}
	if _, err := f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	input.ManagedTarget = ""
	input.ConfirmManagedTarget = false
	input.Visibility = "public"
	value, err := f.service.SaveLocation(t.Context(), f.actor, input)
	if err != nil || !value.HasSource || value.Visibility != "public" {
		t.Fatal(value, err)
	}
	input.Purge = true
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
}
