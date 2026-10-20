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

func TestRemoteWorkerCacheRetryLastKnownGood(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	token := "SECRET"
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "ipinfo_https", RemoteConsent: true, Credential: &token, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	status := 200
	geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "ipinfo.io" || r.URL.Query().Get("token") != token {
			t.Fatal("provider contract")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"country":"US"}`)), Header: make(http.Header)}, nil
	})})
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Refresh(t.Context(), f.actor, "geo-node"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cache missed: %d", calls)
	}
	f.now = f.now.Add(91 * 24 * time.Hour)
	status = 503
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	// 过期会话只影响管理读取，恢复fixture会话后检查后台结果。
	f.exec(t, `UPDATE admin_sessions SET idle_expires_at=?,absolute_expires_at=?,reauthenticated_until=?`, f.now.Add(time.Hour), f.now.Add(time.Hour), f.now.Add(time.Hour))
	value, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || value.CountryCode != "US" || !value.Stale || value.ErrorCode != "provider_unavailable" {
		t.Fatal(value, err)
	}
	if value.NextRefreshAt == nil || !value.NextRefreshAt.Equal(f.now.Add(5*time.Minute)) {
		t.Fatal("retry scheduling", value)
	}
	f.observe(t, "192.168.1.5")
	if err = f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("private address reached provider", calls)
	}
}

func TestManagedTargetAndRuntimeCancellation(t *testing.T) {
	f := newFixture(t)
	input := geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "managed_target", ManagedTarget: "10.1.2.3", Visibility: "private", ConfirmManagedTarget: true}}
	if _, err := f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || value.Scope != "private" {
		t.Fatal(value, err)
	}
	if err = f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.service.Run(ctx) }()
	cancel()
	select {
	case err = <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}
}

func TestExpiredLeaseAndDeletedNode(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "10.0.0.1")
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE node_locations SET lease_hash='dead',lease_expires_at=? WHERE id='geo-node'`, f.now.Add(-time.Second))
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || value.Scope != "private" {
		t.Fatal(value, err)
	}
	f.exec(t, `DELETE FROM nodes WHERE id='geo-node'`)
	if public, err := f.service.PublicLocation(t.Context(), "geo-node"); err != nil || public != nil {
		t.Fatal(public, err)
	}
}

func TestRuntimeDrainsDueLocalWorkWithoutOneSecondDelay(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "10.0.0.1")
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.service.Run(ctx) }()
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		snapshot, err := f.service.Snapshot(ctx)
		if err == nil && snapshot.CacheEntries == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("runtime did not drain local work")
		case <-timer.C:
		}
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
}

func TestProcessObserverReportsCommittedOutcome(t *testing.T) {
	f := newFixture(t)
	observed := []error{}
	f.service.SetObserver(func(err error) { observed = append(observed, err) })
	if err := f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0] != nil || observed[1] != nil {
		t.Fatal(observed)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Process(t.Context()); err == nil {
		t.Fatal("closed process")
	}
	if len(observed) != 3 || observed[2] == nil {
		t.Fatal(observed)
	}
}

func TestProviderFailureWithPersistedRetryReportsProgress(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})})
	calls := 0
	f.service.SetObserver(func(err error) {
		calls++
		if err != nil {
			t.Fatal("persisted provider failure reported as storage failure", err)
		}
	})
	if err := f.service.Process(t.Context()); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	value, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || value.ErrorCode != "provider_unavailable" || value.NextRefreshAt == nil {
		t.Fatal(value, err)
	}
}
