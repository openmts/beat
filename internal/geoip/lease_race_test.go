package geoip_test

import (
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

func TestClaimCompletionRechecksSourceLeaseAndProvider(t *testing.T) {
	for _, query := range []string{
		`UPDATE node_locations SET source_fingerprint='replaced'`,
		`UPDATE node_locations SET lease_hash='reclaimed'`,
		`UPDATE geoip_settings SET revision=revision+1`,
		`DELETE FROM nodes WHERE id='geo-node'`,
	} {
		t.Run(query, func(t *testing.T) {
			f := newFixture(t)
			f.observe(t, "8.8.8.8")
			if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
				t.Fatal(err)
			}
			err := geoip.CompleteClaimForTest(f.service, t.Context(), func() error { _, err := f.db.DB.Exec(query); return err })
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err = f.db.DB.QueryRow(`SELECT COUNT(*) FROM node_locations WHERE country_code!=''`).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale lease committed", count, err)
			}
		})
	}
}

func TestWorkerStorageAndCredentialFailures(t *testing.T) {
	for _, failure := range []string{"claim", "credential", "cache", "settings"} {
		t.Run(failure, func(t *testing.T) {
			f := newFixture(t)
			f.observe(t, "10.0.0.1")
			token := "TOKEN"
			if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "ipinfo_https", RemoteConsent: true, Credential: &token, ExpectedRevision: 1}); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "claim":
				f.exec(t, `CREATE TRIGGER fail BEFORE UPDATE OF lease_hash ON node_locations BEGIN SELECT RAISE(ABORT,'fail');END`)
			case "credential":
				f.exec(t, `UPDATE geoip_settings SET credential_encrypted=X'00'`)
			case "cache":
				f.exec(t, `CREATE TRIGGER fail BEFORE INSERT ON geoip_cache BEGIN SELECT RAISE(ABORT,'fail');END`)
			case "settings":
				f.exec(t, `ALTER TABLE geoip_settings RENAME COLUMN provider TO inaccessible`)
			}
			if err := f.service.Process(t.Context()); err == nil {
				t.Fatal("fault worker success")
			}
		})
	}
}
