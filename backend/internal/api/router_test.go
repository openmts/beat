package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func setupTestRouter(t *testing.T) (*Router, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	nodeStore := store.NewNodeStore(sqliteStore.DB)
	groupStore := store.NewGroupStore(sqliteStore.DB)
	sshKeyStore := store.NewSSHKeyStore(sqliteStore.DB)
	alertRuleStore := store.NewAlertRuleStore(sqliteStore.DB)
	alertChannelStore := store.NewAlertChannelStore(sqliteStore.DB)
	alertEventStore := store.NewAlertEventStore(sqliteStore.DB)

	router := NewRouter(nodeStore, groupStore, sshKeyStore, alertRuleStore, alertChannelStore, alertEventStore, nil)
	cleanup := func() { _ = sqliteStore.Close() }
	return router, cleanup
}

func TestNewRouter(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.mux == nil {
		t.Fatal("expected non-nil mux")
	}
	if router.nodeStore == nil {
		t.Error("expected nodeStore to be set")
	}
	if router.groupStore == nil {
		t.Error("expected groupStore to be set")
	}
	if router.sshKeyStore == nil {
		t.Error("expected sshKeyStore to be set")
	}
	if router.alertRuleStore == nil {
		t.Error("expected alertRuleStore to be set")
	}
	if router.alertChannelStore == nil {
		t.Error("expected alertChannelStore to be set")
	}
	if router.alertEventStore == nil {
		t.Error("expected alertEventStore to be set")
	}
}

func TestRouterServeHandler(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	handler := router.ServeHandler()
	if handler == nil {
		t.Fatal("expected non-nil handler from ServeHandler")
	}
}

func TestRouterNodesRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET /api/v1/nodes, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":200`) {
		t.Errorf("expected JSON response with code 200, got: %s", body)
	}
}

func TestRouterNodeByIDRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/nonexistent-id", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for GET /api/v1/nodes/nonexistent-id, got %d", w.Code)
	}
}

func TestRouterGroupsRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET /api/v1/groups, got %d", w.Code)
	}
}

func TestRouterCreateGroupRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	body := `{"name":"test-group"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201 for POST /api/v1/groups, got %d", w.Code)
	}
}

func TestRouterSSHKeysRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ssh-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET /api/v1/ssh-keys, got %d", w.Code)
	}
}

func TestRouterAlertRulesRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/rules", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET /api/v1/alerts/rules, got %d", w.Code)
	}
}

func TestRouterAlertEventsRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/events", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET /api/v1/alerts/events, got %d", w.Code)
	}
}

func TestRouterCORSMiddleware(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("expected CORS header Access-Control-Allow-Origin: *, got %s", origin)
	}
}

func TestRouterPreflightCORS(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for OPTIONS request, got %d", w.Code)
	}
}

func TestRouterRecoveryMiddleware(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("router should recover from panics, but panic was: %v", r)
		}
	}()

	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 after recovery, got %d", w.Code)
	}
}

func TestRouterNotFound(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown route, got %d", w.Code)
	}
}

func TestRouterUpdateGroupRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	body := `{"name":"test-group"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201 for creating group, got %d", w.Code)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	groupID := resp.Data.ID
	if groupID == "" {
		t.Fatal("expected non-empty group ID in response")
	}
}

func TestRouterDeleteGroupRoute(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	body := `{"name":"to-delete"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for creating group, got %d", w.Code)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	groupID := resp.Data.ID

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/groups/"+groupID, nil)
	req.SetPathValue("id", groupID)
	w = httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for DELETE group, got %d", w.Code)
	}
}

func TestRouterHealthEndpoint(t *testing.T) {
	router, cleanup := setupTestRouter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for /api/v1/health (not registered), got %d", w.Code)
	}
}
