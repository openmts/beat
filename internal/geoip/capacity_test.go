package geoip_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

func TestCacheHardCapEvictsLeastRecentlyUsed(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "10.0.0.1")
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `WITH RECURSIVE items(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM items WHERE x<100001)
 INSERT INTO geoip_cache(provider_revision,source_fingerprint,scope,country_code,negative,resolved_at,expires_at,accessed_at)
 SELECT 'old',printf('%06d',x),'public','US',0,?,?,? FROM items`, f.now.Add(-time.Hour), f.now.Add(time.Hour), f.now.Add(-time.Hour))
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.DB.QueryRow(`SELECT COUNT(*) FROM geoip_cache`).Scan(&count); err != nil || count != 100000 {
		t.Fatal(count, err)
	}
	if err := f.db.DB.QueryRow(`SELECT COUNT(*) FROM geoip_cache WHERE provider_revision!='old'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("new cache evicted", count, err)
	}
}

func TestConsentRevocationWaitsForPriorRequestAndStopsNewWork(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"country":"US"}`)), Header: make(http.Header)}, nil
	})})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	processed := make(chan error, 1)
	go func() { processed <- f.service.Process(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	revoked := make(chan error, 1)
	go func() {
		_, err := f.service.SaveSettings(ctx, f.actor, model.GeoIPSettingsWrite{Provider: "disabled", ExpectedRevision: 2})
		revoked <- err
	}()
	close(release)
	if err := <-processed; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	f.observe(t, "8.8.4.4")
	if err := f.service.Process(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotAndBoundedCacheMaintenance(t *testing.T) {
	f := newFixture(t)
	snapshot, err := f.service.Snapshot(t.Context())
	if err != nil || snapshot.CacheEntries != 0 || snapshot.Provider != "disabled" {
		t.Fatal(snapshot, err)
	}
	f.exec(t, `WITH RECURSIVE items(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM items WHERE x<1100)
 INSERT INTO geoip_cache(provider_revision,source_fingerprint,scope,country_code,negative,resolved_at,expires_at,accessed_at)
 SELECT 'old',printf('%06d',x),'public','US',0,?,?,? FROM items`, f.now.Add(-time.Hour), f.now.Add(-time.Hour), f.now.Add(-time.Hour))
	if err = f.service.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.service.Snapshot(t.Context())
	if err != nil || snapshot.CacheEntries != 100 {
		t.Fatal(snapshot, err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Prune(t.Context()); err != geoip.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err = f.service.Snapshot(t.Context()); err != geoip.ErrUnavailable {
		t.Fatal(err)
	}
}
