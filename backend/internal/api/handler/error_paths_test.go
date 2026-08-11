package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type handlerTestStores struct {
	nodes    *store.NodeStore
	groups   *store.GroupStore
	keys     *store.SSHKeyStore
	rules    *store.AlertRuleStore
	channels *store.AlertChannelStore
	events   *store.AlertEventStore
}

func closedHandlerStores(t *testing.T) handlerTestStores {
	t.Helper()
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	return handlerTestStores{
		nodes: store.NewNodeStore(s.DB), groups: store.NewGroupStore(s.DB),
		keys: store.NewSSHKeyStore(s.DB), rules: store.NewAlertRuleStore(s.DB),
		channels: store.NewAlertChannelStore(s.DB), events: store.NewAlertEventStore(s.DB),
	}
}

func runHandlerRequest(
	t *testing.T,
	handler http.HandlerFunc,
	method string,
	body string,
	pathValues map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req = req.WithContext(middleware.WithAgentIdentity(req.Context(), model.AgentIdentity{
		NodeID: "id", NodeName: "node", Mode: model.AgentCredentialActive,
	}))
	for key, value := range pathValues {
		req.SetPathValue(key, value)
	}
	w := httptest.NewRecorder()
	handler(w, req)

	return w
}

func TestGroupHandlerDatabaseErrors(t *testing.T) {
	stores := closedHandlerStores(t)
	h := NewGroupHandler(stores.groups)
	tests := []struct {
		name, method, body string
		handler            http.HandlerFunc
		pathValues         map[string]string
	}{
		{"list", http.MethodGet, "", h.HandleListGroups, nil},
		{"create", http.MethodPost, `{"name":"group"}`, h.HandleCreateGroup, nil},
		{"update", http.MethodPut, `{"name":"group"}`, h.HandleUpdateGroup, map[string]string{"id": "id"}},
		{"delete", http.MethodDelete, "", h.HandleDeleteGroup, map[string]string{"id": "id"}},
		{"sort", http.MethodPut, `{"ids":["id"]}`, h.HandleUpdateSortOrder, nil},
		{"default", http.MethodPut, "", h.HandleSetDefaultGroup, map[string]string{"id": "id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := runHandlerRequest(t, tt.handler, tt.method, tt.body, tt.pathValues)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestNodeHandlerDatabaseErrors(t *testing.T) {
	stores := closedHandlerStores(t)
	h := NewNodeHandler(stores.nodes, nil)
	tests := []struct {
		name, method, body string
		handler            http.HandlerFunc
		pathValues         map[string]string
	}{
		{"list", http.MethodGet, "", h.HandleListNodes, nil},
		{"get", http.MethodGet, "", h.HandleGetNode, map[string]string{"id": "id"}},
		{"update", http.MethodPut, `{}`, h.HandleUpdateNode, map[string]string{"id": "id"}},
		{"delete", http.MethodDelete, "", h.HandleDeleteNode, map[string]string{"id": "id"}},
		{"report", http.MethodPost, `{"name":"node","host":"127.0.0.1","port":22}`, h.HandleNodeReport, nil},
		{"metrics", http.MethodGet, "", h.HandleGetNodeMetrics, map[string]string{"id": "id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := runHandlerRequest(t, tt.handler, tt.method, tt.body, tt.pathValues)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestNodeReportMetricsStoreUnavailable(t *testing.T) {
	s := setupTestDB(t)
	nodes := store.NewNodeStore(s.DB)
	node, err := nodes.UpsertNode(t.Context(), "node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	h := NewNodeHandler(nodes, nil)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"name":"node","host":"127.0.0.1","port":22,"metrics":{"cpu":1}}`,
	))
	request = request.WithContext(middleware.WithAgentIdentity(request.Context(), model.AgentIdentity{
		NodeID: node.ID, NodeName: node.Name, Mode: model.AgentCredentialActive,
	}))
	w := httptest.NewRecorder()
	h.HandleNodeReport(w, request)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestNodeHandlerAdditionalErrorAndMetricPaths(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := sqliteStore.DB.ExecContext(t.Context(), "UPDATE nodes SET is_public = 1 WHERE id = ?", node.ID); err != nil {
		t.Fatalf("publish node: %v", err)
	}
	malformed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("{"))
	NewNodeHandler(nodes, nil).HandleUpdateNode(malformed, request)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed update status = %d", malformed.Code)
	}
	unauthenticated := httptest.NewRecorder()
	NewNodeHandler(nodes, nil).HandleNodeReport(unauthenticated,
		httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"host":"127.0.0.1","port":22}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated report status = %d", unauthenticated.Code)
	}
	assertUnknownAndFailedMetricReports(t, nodes, node)
	assertNodeSettingsAndMetricResponses(t, nodes, node)
}

func assertUnknownAndFailedMetricReports(t *testing.T, nodes *store.NodeStore, node *model.Node) {
	t.Helper()
	unknown := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"host":"127.0.0.1","port":22}`))
	unknown = unknown.WithContext(middleware.WithAgentIdentity(unknown.Context(), model.AgentIdentity{
		NodeID: "missing", NodeName: "missing", Mode: model.AgentCredentialActive,
	}))
	response := httptest.NewRecorder()
	NewNodeHandler(nodes, nil).HandleNodeReport(response, unknown)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown agent report status = %d", response.Code)
	}
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "closed-mts"))
	if err != nil {
		t.Fatalf("new closed MTS: %v", err)
	}
	if err := mtsStore.Close(); err != nil {
		t.Fatalf("close MTS: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"host":"127.0.0.1","port":22,"metrics":{"cpu":1}}`,
	))
	request = request.WithContext(middleware.WithAgentIdentity(request.Context(), model.AgentIdentity{
		NodeID: node.ID, NodeName: node.Name, Mode: model.AgentCredentialActive,
	}))
	response = httptest.NewRecorder()
	NewNodeHandler(nodes, mtsStore).HandleNodeReport(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed MTS report status = %d", response.Code)
	}
}

func assertNodeSettingsAndMetricResponses(
	t *testing.T, nodes *store.NodeStore, node *model.Node,
) {
	t.Helper()
	settingsSQLite := setupTestDB(t)
	closedSettings := store.NewSiteSettingsStore(settingsSQLite.DB)
	if err := settingsSQLite.Close(); err != nil {
		t.Fatalf("close settings database: %v", err)
	}
	for _, call := range []func(http.ResponseWriter, *http.Request){
		NewNodeHandler(nodes, nil, closedSettings).HandleListNodes,
		NewNodeHandler(nodes, nil, closedSettings).HandleGetNode,
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.SetPathValue("id", node.ID)
		response := httptest.NewRecorder()
		call(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("closed settings status = %d", response.Code)
		}
	}

	metricSQLite := setupTestDB(t)
	metricNodes := store.NewNodeStore(metricSQLite.DB)
	metricNode, err := metricNodes.UpsertNode(t.Context(), "metric-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create metric node: %v", err)
	}
	if _, err := metricSQLite.DB.ExecContext(t.Context(), "UPDATE nodes SET is_public = 1 WHERE id = ?", metricNode.ID); err != nil {
		t.Fatalf("publish metric node: %v", err)
	}
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "metrics"))
	if err != nil {
		t.Fatalf("new metrics store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	now := time.Now().UTC()
	if err := mtsStore.WriteMetric(t.Context(), metricNode.ID, "cpu", 42, now); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/?metric=cpu&from="+
		now.Add(-time.Minute).Format(time.RFC3339)+"&to="+now.Add(time.Minute).Format(time.RFC3339), nil)
	request.SetPathValue("id", metricNode.ID)
	response := httptest.NewRecorder()
	NewNodeHandler(metricNodes, mtsStore).HandleGetNodeMetrics(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"value":42`) {
		t.Fatalf("metric response = %d, %s", response.Code, response.Body.String())
	}
}

func TestSSHKeyHandlerDatabaseErrors(t *testing.T) {
	stores := closedHandlerStores(t)
	h := NewSSHKeyHandler(stores.keys)
	tests := []struct {
		name, method, body string
		handler            http.HandlerFunc
		pathValues         map[string]string
	}{
		{"list", http.MethodGet, "", h.HandleListSSHKeys, nil},
		{"create", http.MethodPost, `{"name":"key","public_key":"ssh-ed25519 AAAA"}`, h.HandleCreateSSHKey, nil},
		{"generate", http.MethodPost, `{"name":"key","key_type":"ed25519"}`, h.HandleGenerateSSHKey, nil},
		{"delete", http.MethodDelete, "", h.HandleDeleteSSHKey, map[string]string{"id": "id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := runHandlerRequest(t, tt.handler, tt.method, tt.body, tt.pathValues)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestAlertHandlerDatabaseErrors(t *testing.T) {
	stores := closedHandlerStores(t)
	h := NewAlertHandler(stores.rules, stores.channels, stores.events)
	tests := []struct {
		name, method, body string
		handler            http.HandlerFunc
		pathValues         map[string]string
	}{
		{"list rules", http.MethodGet, "", h.HandleListAlertRules, nil},
		{"create rule", http.MethodPost, `{"name":"rule","metric":"cpu","operator":"gt"}`, h.HandleCreateAlertRule, nil},
		{"update rule", http.MethodPut, `{}`, h.HandleUpdateAlertRule, map[string]string{"id": "id"}},
		{"delete rule", http.MethodDelete, "", h.HandleDeleteAlertRule, map[string]string{"id": "id"}},
		{"list channels", http.MethodGet, "", h.HandleListAlertChannels, nil},
		{"create channel", http.MethodPost, `{"name":"channel","channel_type":"webhook","config":"https://example.com/hook"}`, h.HandleCreateAlertChannel, nil},
		{"update channel", http.MethodPut, `{"name":"channel","channel_type":"webhook","config":"https://example.com/hook"}`, h.HandleUpdateAlertChannel, map[string]string{"id": "id"}},
		{"delete channel", http.MethodDelete, "", h.HandleDeleteAlertChannel, map[string]string{"id": "id"}},
		{"list events", http.MethodGet, "", h.HandleListAlertEvents, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := runHandlerRequest(t, tt.handler, tt.method, tt.body, tt.pathValues)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestClosedHandlerStoresUseContext(t *testing.T) {
	stores := closedHandlerStores(t)
	if _, err := stores.nodes.ListNodes(context.Background(), ""); err == nil {
		t.Fatal("expected closed database error")
	}
}
