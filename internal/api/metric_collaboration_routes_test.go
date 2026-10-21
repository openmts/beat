package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func newCollaborationRouter(t *testing.T) (*Router, *store.SQLiteStore, string) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	random := bytes.NewReader(bytes.Repeat([]byte{7}, 8192))
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(sqlite.DB),
		Secrets: testutil.NewSecrets(t, sqlite.DB), LegacyToken: "admin-secret", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, random)})
	if err != nil {
		t.Fatal(err)
	}
	nodes, catalog := store.NewNodeStore(sqlite.DB), store.NewMetricCatalogStore(sqlite.DB)
	router := NewRouter(nodes, store.NewGroupStore(sqlite.DB), store.NewSSHKeyStore(sqlite.DB),
		store.NewAlertRuleStore(sqlite.DB), store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		WithAdminToken("admin-secret"), WithAdminSecurity(security),
		WithMetricDashboards(store.NewDashboardStore(sqlite.DB), handler.NewDashboardPolicyReader(catalog, nodes)))
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`,
		"", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate status = %d, body = %s", reauth.Code, reauth.Body.String())
	}
	return router, sqlite, cookie.Value
}

func bootstrapCookie(t *testing.T, value string) *http.Cookie {
	t.Helper()
	return &http.Cookie{Name: handler.AdminSessionCookieName, Value: value}
}

func createCollaborationDashboard(t *testing.T, router *Router, cookie string) string {
	t.Helper()
	definition, err := json.Marshal(model.DefaultMetricDashboard("personal").Definition)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"revision":0,"definition":` + string(definition) + `}`
	response := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/metrics/dashboards", body,
		bootstrapCookie(t, cookie), "http://example.com")
	if response.Code != http.StatusCreated {
		t.Fatalf("create dashboard status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.ID
}

func TestAnnotationRoutesLifecycle(t *testing.T) {
	router, _, cookie := newCollaborationRouter(t)
	now := model.NowUTC().Truncate(time.Second)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	create := fmt.Sprintf(`{"dashboard_id":"public-default","title":"Deploy","text":"v1","tags":["deploy"],"color":"blue",`+
		`"visibility":"public","from":"%s","to":"%s"}`, from.Format(time.RFC3339), to.Format(time.RFC3339))
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/metrics/annotations", create,
		bootstrapCookie(t, cookie), "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create annotation status = %d, body = %s", created.Code, created.Body.String())
	}
	list := requestRouterWithCookie(router, http.MethodGet,
		fmt.Sprintf("/api/v1/admin/metrics/annotations?dashboard_id=public-default&from=%s&to=%s",
			now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339)),
		"", bootstrapCookie(t, cookie), "")
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"title":"Deploy"`)) {
		t.Fatalf("list annotation status = %d, body = %s", list.Code, list.Body.String())
	}
	update := fmt.Sprintf(`{"dashboard_id":"public-default","title":"Deploy 2","text":"","tags":[],"color":"amber",`+
		`"visibility":"public","from":"%s","to":"%s"}`, from.Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339))
	updated := requestRouterWithCookie(router, http.MethodPut, annotationPath(created.Body.Bytes()), update,
		bootstrapCookie(t, cookie), "http://example.com")
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"title":"Deploy 2"`)) {
		t.Fatalf("update annotation status = %d, body = %s", updated.Code, updated.Body.String())
	}
	public := requestRouter(router, http.MethodGet, "/api/v1/metrics/annotations?dashboard_id=public-default", "", "", "")
	if public.Code != http.StatusOK || !bytes.Contains(public.Body.Bytes(), []byte(`"title":"Deploy 2"`)) {
		t.Fatalf("public annotation status = %d, body = %s", public.Code, public.Body.String())
	}
	deleted := requestRouterWithCookie(router, http.MethodDelete, annotationPath(created.Body.Bytes()), "",
		bootstrapCookie(t, cookie), "http://example.com")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete annotation status = %d", deleted.Code)
	}
}

func annotationPath(body []byte) string {
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &payload)
	return "/api/v1/admin/metrics/annotations/" + payload.Data.ID
}

func TestSnapshotRoutesFreezeAndRevoke(t *testing.T) {
	router, _, cookie := newCollaborationRouter(t)
	dashboardID := createCollaborationDashboard(t, router, cookie)
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/metrics/snapshots",
		`{"dashboard_id":"`+dashboardID+`","name":"Freeze"}`, bootstrapCookie(t, cookie), "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create snapshot status = %d, body = %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	public := requestRouter(router, http.MethodGet, "/api/v1/metrics/snapshots/"+payload.Data.Token, "", "", "")
	if public.Code != http.StatusOK || !bytes.Contains(public.Body.Bytes(), []byte(`"name":"Freeze"`)) {
		t.Fatalf("public snapshot status = %d, body = %s", public.Code, public.Body.String())
	}
	if bytes.Contains(public.Body.Bytes(), []byte("token")) {
		t.Fatal("public snapshot response must not expose the share token")
	}
	revoked := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/metrics/snapshots/"+payload.Data.ID, "",
		bootstrapCookie(t, cookie), "http://example.com")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke snapshot status = %d", revoked.Code)
	}
	gone := requestRouter(router, http.MethodGet, "/api/v1/metrics/snapshots/"+payload.Data.Token, "", "", "")
	if gone.Code != http.StatusNotFound {
		t.Fatalf("revoked snapshot status = %d", gone.Code)
	}
}

func TestLibraryPanelRoutesAndPanelAlerts(t *testing.T) {
	router, _, cookie := newCollaborationRouter(t)
	dashboardID := createCollaborationDashboard(t, router, cookie)
	panel := model.DefaultMetricDashboard("personal").Definition.Panels[0]
	encoded, err := json.Marshal(panel)
	if err != nil {
		t.Fatal(err)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/metrics/panels",
		`{"name":"Saved CPU","panel":`+string(encoded)+`}`, bootstrapCookie(t, cookie), "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create library panel status = %d, body = %s", created.Code, created.Body.String())
	}
	list := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/metrics/panels", "",
		bootstrapCookie(t, cookie), "")
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"name":"Saved CPU"`)) {
		t.Fatalf("list library panels status = %d, body = %s", list.Code, list.Body.String())
	}
	linked := requestRouterWithCookie(router, http.MethodPut,
		"/api/v1/admin/metrics/dashboards/"+dashboardID+"/panel-alerts/"+panel.ID, `{"rule_ids":[]}`,
		bootstrapCookie(t, cookie), "http://example.com")
	if linked.Code != http.StatusOK {
		t.Fatalf("replace panel alerts status = %d, body = %s", linked.Code, linked.Body.String())
	}
	missing := requestRouterWithCookie(router, http.MethodPut,
		"/api/v1/admin/metrics/dashboards/"+dashboardID+"/panel-alerts/"+panel.ID, `{"rule_ids":["missing"]}`,
		bootstrapCookie(t, cookie), "http://example.com")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown rule link status = %d", missing.Code)
	}
	links := requestRouterWithCookie(router, http.MethodGet,
		"/api/v1/admin/metrics/dashboards/"+dashboardID+"/panel-alerts", "", bootstrapCookie(t, cookie), "")
	if links.Code != http.StatusOK || !bytes.Contains(links.Body.Bytes(), []byte(`"data":[]`)) {
		t.Fatalf("list panel alerts status = %d, body = %s", links.Code, links.Body.String())
	}
}
