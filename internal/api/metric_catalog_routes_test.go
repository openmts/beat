package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func setupMetricCatalogRouter(t *testing.T) *Router {
	t.Helper()
	return setupSessionRouterWithOptions(t, func(router *Router) {
		sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlite.Close() })
		metrics, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = metrics.Close() })
		router.mtsStore = metrics
		WithMetricCatalogStore(store.NewMetricCatalogStore(sqlite.DB))(router)
	})
}

func metricRoutePolicy(definition *model.MetricDefinition) model.MetricPolicyUpdate {
	return model.MetricPolicyUpdate{Revision: definition.Revision, Label: definition.Label, Description: definition.Description,
		DefaultAggregation: definition.DefaultAggregation, Public: definition.Public, Enabled: definition.Enabled, RetentionDays: definition.RetentionOverride}
}

func TestMetricCatalogRoutesRequireOwnerRecentAuthAndPreservePublicPrivacy(t *testing.T) {
	router := setupMetricCatalogRouter(t)
	cookie := enrollmentOwnerCookie(t, router)
	body := `{"revision":1,"label":"Private CPU","description":"","default_aggregation":"avg","public":false,"enabled":true,"retention_days":null}`
	path := "/api/v1/admin/metrics/catalog/cpu"
	response := requestRouter(router, http.MethodPut, path, body, "Bearer admin-secret", "http://example.com")
	if response.Code != http.StatusUnauthorized {
		t.Fatal("bootstrap bearer bypassed catalog owner guard")
	}
	response = requestRouterWithCookie(router, http.MethodPut, path, body, cookie, "http://other.example.com")
	if response.Code != http.StatusForbidden {
		t.Fatal("cross-origin catalog mutation accepted")
	}
	response = requestRouterWithCookie(router, http.MethodPut, path, body, cookie, "http://example.com")
	if response.Code != http.StatusPreconditionRequired {
		t.Fatal("catalog mutation did not require recent authentication")
	}
	response = requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if response.Code != http.StatusOK {
		t.Fatal("owner could not reauthenticate")
	}
	response = requestRouterWithCookie(router, http.MethodPut, path, body, cookie, "http://example.com")
	if response.Code != http.StatusOK {
		t.Fatalf("authorized policy update=%d/%s", response.Code, response.Body.String())
	}
	public := requestRouter(router, http.MethodGet, "/api/v1/metrics/catalog", "", "", "")
	admin := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/metrics/catalog", "", cookie, "")
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), `"key":"cpu"`) {
		t.Fatal("public catalog exposed private metric")
	}
	if admin.Code != http.StatusOK || !strings.Contains(admin.Body.String(), `"retention_override_days":null`) {
		t.Fatal("admin catalog lost inherited retention")
	}
	response = requestRouterWithCookie(router, http.MethodPut, path, body, cookie, "http://example.com")
	if response.Code != http.StatusConflict {
		t.Fatal("stale revision overwrote catalog")
	}
}

func TestMetricQueryRoutesReadWithoutLoginAndHidePrivateLegacySnapshots(t *testing.T) {
	router := setupMetricCatalogRouter(t)
	node, err := router.nodeStore.UpsertNode(t.Context(), "public-query", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := router.mtsStore.WriteMetric(t.Context(), node.ID, "cpu", 25, now); err != nil {
		t.Fatal(err)
	}
	body := `{"from":"` + now.Add(-time.Minute).Format(time.RFC3339) + `","to":"` + now.Add(time.Second).Format(time.RFC3339) + `","max_points":5,"series":[{"ref_id":"A","node_id":"` + node.ID + `","metric":"cpu","aggregation":"avg"}]}`
	response := requestRouter(router, http.MethodPost, "/api/v1/metrics/query", body, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"value":25`) {
		t.Fatalf("public query=%d/%s", response.Code, response.Body.String())
	}
	definition, err := router.metricCatalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	policy := metricRoutePolicy(definition)
	policy.Public = false
	owner := boundaryOwner(t, router)
	if _, err := router.metricCatalog.UpdatePolicy(t.Context(), "cpu", store.MetricCatalogPolicyChange{Policy: policy, SessionID: owner.Principal.Session.ID, Audit: model.AdminAuditEvent{ActorID: owner.Principal.User.ID}}); err != nil {
		t.Fatal(err)
	}
	response = requestRouter(router, http.MethodPost, "/api/v1/metrics/query", body, "", "")
	if response.Code != http.StatusBadRequest {
		t.Fatal("public query read private metric")
	}
	response = requestRouter(router, http.MethodGet, "/api/v1/nodes/"+node.ID, "", "", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"cpu":`) {
		t.Fatal("private value escaped via legacy node snapshot")
	}
}
