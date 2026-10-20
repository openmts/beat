package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func newPublicInteractionRouter(t *testing.T) (*Router, *store.PublicTelemetryStore) {
	t.Helper()
	directory := t.TempDir()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	mtsStore, err := store.NewMTSStore(filepath.Join(directory, "mts"))
	if err != nil {
		_ = sqliteStore.Close()
		t.Fatalf("create mts store: %v", err)
	}
	t.Cleanup(func() {
		_ = mtsStore.Close()
		_ = sqliteStore.Close()
	})
	policies := store.NewPublicTelemetryStore(sqliteStore.DB)
	router := NewRouter(store.NewNodeStore(sqliteStore.DB), store.NewGroupStore(sqliteStore.DB),
		store.NewSSHKeyStore(sqliteStore.DB), store.NewAlertRuleStore(sqliteStore.DB),
		store.NewAlertChannelStore(sqliteStore.DB), store.NewAlertEventStore(sqliteStore.DB), mtsStore,
		WithPublicTelemetryStore(policies), WithSiteSettingsStore(store.NewSiteSettingsStore(sqliteStore.DB)))
	return router, policies
}

func postInteraction(t *testing.T, router *Router) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/interactions",
		strings.NewReader(`{"event":"page_view","surface":"dashboard"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(recorder, request)
	return recorder
}

func TestPublicInteractionRouteFollowsPolicy(t *testing.T) {
	router, policies := newPublicInteractionRouter(t)
	if expected, actual := http.StatusBadRequest, postInteraction(t, router).Code; expected != actual {
		t.Fatalf("disabled status = %d, want %d", actual, expected)
	}

	policy, err := policies.Get(t.Context())
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	policy.Enabled = true
	policy.Revision++
	if _, err := policies.Update(t.Context(), policy); err != nil {
		t.Fatalf("enable policy: %v", err)
	}
	if expected, actual := http.StatusNoContent, postInteraction(t, router).Code; expected != actual {
		t.Fatalf("enabled status = %d, want %d", actual, expected)
	}

	siteRequest := httptest.NewRequest(http.MethodGet, "/api/v1/settings/site", nil)
	recorder := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(recorder, siteRequest)
	if !strings.Contains(recorder.Body.String(), `"public_telemetry":{"enabled":true`) {
		t.Fatalf("public settings = %s", recorder.Body.String())
	}
}

func TestPublicInteractionRoutePreflightIsAnonymousOnly(t *testing.T) {
	router, _ := newPublicInteractionRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/public/interactions", nil)
	request.Header.Set("Origin", "https://console.example.com")
	router.ServeHandler().ServeHTTP(recorder, request)
	if expected, actual := http.StatusNoContent, recorder.Code; expected != actual {
		t.Fatalf("preflight status = %d, want %d", actual, expected)
	}
	if methods := recorder.Header().Get("Access-Control-Allow-Methods"); methods != "POST, OPTIONS" {
		t.Fatalf("preflight methods = %q, want %q", methods, "POST, OPTIONS")
	}
	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Fatalf("preflight origin = %q, want *", origin)
	}
}
