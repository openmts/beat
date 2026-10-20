package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/applicationgc"
	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
	"github.com/beat/backend/internal/trafficreport"
)

func setupAuthenticatedRouter(t *testing.T) *Router {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	nodeStore := store.NewNodeStore(sqliteStore.DB)
	if _, err := nodeStore.UpsertNode(t.Context(), "node", "127.0.0.1", 22); err != nil {
		t.Fatalf("create legacy node: %v", err)
	}
	alertChannels := store.NewAlertChannelStore(sqliteStore.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: sqliteStore.DB, Secrets: testutil.NewSecrets(t, sqliteStore.DB)})
	if err != nil {
		t.Fatal(err)
	}
	reports := trafficreport.NewService(trafficreport.Config{Schedules: store.NewTrafficReportScheduleStore(sqliteStore.DB), Nodes: nodeStore})
	summaries := fleetsummary.NewService(fleetsummary.Config{
		Schedules: store.NewFleetSummaryStore(sqliteStore.DB), Nodes: nodeStore,
		Groups: store.NewGroupStore(sqliteStore.DB), Queue: queue})
	retention := applicationgc.New(t.Context(), store.NewApplicationRetentionStore(sqliteStore.DB))
	return NewRouter(
		nodeStore,
		store.NewGroupStore(sqliteStore.DB),
		store.NewSSHKeyStore(sqliteStore.DB),
		store.NewAlertRuleStore(sqliteStore.DB),
		alertChannels,
		store.NewAlertEventStore(sqliteStore.DB),
		reportMetricsFixture(t),
		WithAuthTokens("admin-secret", "agent-secret"),
		WithNotificationQueue(queue),
		WithTrafficReportService(reports),
		WithFleetSummaryService(summaries),
		WithApplicationRetentionService(retention),
		WithSiteSettingsStore(store.NewSiteSettingsStore(sqliteStore.DB)),
		WithMaintenanceService(optionMaintenance{}),
	)
}

func TestRouterAuthentication(t *testing.T) {
	router := setupAuthenticatedRouter(t)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		{name: "public nodes", method: http.MethodGet, path: "/api/v1/nodes", want: http.StatusOK},
		{name: "public site settings", method: http.MethodGet, path: "/api/v1/settings/site", want: http.StatusOK},
		{name: "site settings protected", method: http.MethodPut, path: "/api/v1/settings/site", body: `{}`, want: http.StatusUnauthorized},
		{name: "site settings authorized", method: http.MethodPut, path: "/api/v1/settings/site", token: "admin-secret", body: `{"site_title":"Beat","site_description":"Status","logo_url":"","favicon_url":"/favicon.svg","default_theme":"system","public_address_mode":"full","show_network_quality":true}`, want: http.StatusOK},
		{name: "admin validation missing", method: http.MethodGet, path: "/api/v1/auth/admin", want: http.StatusUnauthorized},
		{name: "admin validation valid", method: http.MethodGet, path: "/api/v1/auth/admin", token: "admin-secret", want: http.StatusOK},
		{name: "admin missing", method: http.MethodPost, path: "/api/v1/groups", body: `{"name":"ops"}`, want: http.StatusUnauthorized},
		{name: "admin wrong", method: http.MethodPost, path: "/api/v1/groups", body: `{"name":"ops"}`, token: "wrong", want: http.StatusUnauthorized},
		{name: "admin valid", method: http.MethodPost, path: "/api/v1/groups", body: `{"name":"ops"}`, token: "admin-secret", want: http.StatusCreated},
		{name: "admin read protected", method: http.MethodGet, path: "/api/v1/ssh-keys", want: http.StatusUnauthorized},
		{name: "channel test protected", method: http.MethodPost, path: "/api/v1/alerts/channels/id/test", want: http.StatusUnauthorized},
		{name: "traffic reports protected", method: http.MethodGet, path: "/api/v1/alerts/traffic-reports", want: http.StatusUnauthorized},
		{name: "traffic reports authorized", method: http.MethodGet, path: "/api/v1/alerts/traffic-reports", token: "admin-secret", want: http.StatusOK},
		{name: "fleet summaries protected", method: http.MethodGet, path: "/api/v1/alerts/fleet-summaries", want: http.StatusUnauthorized},
		{name: "fleet summaries authorized", method: http.MethodGet, path: "/api/v1/alerts/fleet-summaries", token: "admin-secret", want: http.StatusOK},
		{name: "fleet summary create validates", method: http.MethodPost, path: "/api/v1/alerts/fleet-summaries", token: "admin-secret", body: `{}`, want: http.StatusBadRequest},
		{name: "application retention protected", method: http.MethodGet, path: "/api/v1/settings/application-retention", want: http.StatusUnauthorized},
		{name: "application retention authorized", method: http.MethodGet, path: "/api/v1/settings/application-retention", token: "admin-secret", want: http.StatusOK},
		{name: "application retention update validates", method: http.MethodPut, path: "/api/v1/settings/application-retention", token: "admin-secret", body: `{}`, want: http.StatusBadRequest},
		{name: "application retention run protected", method: http.MethodPost, path: "/api/v1/settings/application-retention/run", want: http.StatusUnauthorized},
		{name: "maintenance protected", method: http.MethodGet, path: "/api/v1/settings/maintenance", want: http.StatusUnauthorized},
		{name: "maintenance authorized", method: http.MethodGet, path: "/api/v1/settings/maintenance", token: "admin-secret", want: http.StatusOK},
		{name: "maintenance run protected", method: http.MethodPost, path: "/api/v1/settings/maintenance/run", want: http.StatusUnauthorized},
		{name: "agent missing", method: http.MethodPost, path: "/api/v1/nodes/report", body: testutil.VersionedAgentReport(t, `{"name":"node","host":"127.0.0.1","port":22,"metrics":{}}`), want: http.StatusUnauthorized},
		{name: "agent valid", method: http.MethodPost, path: "/api/v1/nodes/report", body: testutil.VersionedAgentReport(t, `{"name":"node","host":"127.0.0.1","port":22,"metrics":{}}`), token: "agent-secret", want: http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			router.ServeHandler().ServeHTTP(response, request)
			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, tt.want, response.Body.String())
			}
		})
	}
}
