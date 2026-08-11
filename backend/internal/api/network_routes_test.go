package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

type networkRouterFixture struct {
	router *Router
	nodeID string
}

type networkRequestSpec struct {
	method string
	path   string
	body   string
	token  string
	want   int
}

func setupNetworkRouter(t *testing.T) networkRouterFixture {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	nodeStore := store.NewNodeStore(sqliteStore.DB)
	node, err := nodeStore.UpsertNode(t.Context(), "route-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	networkStore := store.NewNetworkTaskStore(sqliteStore.DB)
	router := NewRouter(
		nodeStore,
		store.NewGroupStore(sqliteStore.DB),
		store.NewSSHKeyStore(sqliteStore.DB),
		store.NewAlertRuleStore(sqliteStore.DB),
		store.NewAlertChannelStore(sqliteStore.DB),
		store.NewAlertEventStore(sqliteStore.DB),
		mtsStore,
		WithAuthTokens("admin-secret", "agent-secret"),
		WithNetworkTaskStore(networkStore),
	)
	return networkRouterFixture{router: router, nodeID: node.ID}
}

func TestNetworkRoutesAuthAndWorkflow(t *testing.T) {
	fixture := setupNetworkRouter(t)
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/quality", want: http.StatusOK,
	})
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/tasks", want: http.StatusUnauthorized,
	})
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/assignments?node_name=route-node", want: http.StatusUnauthorized,
	})

	createBody := "{\"name\":\"Loopback\",\"type\":\"icmp\",\"target\":\"127.0.0.1\",\"ip_family\":\"auto\"," +
		"\"interval_seconds\":60,\"timeout_milliseconds\":1000,\"all_nodes\":true," +
		"\"enabled\":true,\"is_public\":true,\"sort_order\":0}"
	response := networkRequest(t, fixture.router, networkRequestSpec{
		method: http.MethodPost, path: "/api/v1/network/tasks", body: createBody, token: "admin-secret",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.ID == "" {
		t.Fatal("created task ID is empty")
	}

	response = networkRequest(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/assignments?node_name=route-node", token: "agent-secret",
	})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"Loopback"`) {
		t.Fatalf("assignments status = %d, body = %s", response.Code, response.Body.String())
	}

	resultBody := "{\"node_name\":\"route-node\",\"results\":[{\"task_id\":\"" + created.Data.ID +
		"\",\"finished_at\":\"" + time.Now().UTC().Format(time.RFC3339Nano) +
		"\",\"latency_ms\":1.25,\"success\":true,\"status_code\":0,\"error_code\":\"none\"}]}"
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodPost, path: "/api/v1/network/results", body: resultBody,
		token: "agent-secret", want: http.StatusNoContent,
	})

	response = networkRequest(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/quality",
	})
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"name":"Loopback"`) ||
		!strings.Contains(body, `"latency_ms":1.25`) {
		t.Fatalf("quality status = %d, body = %s", response.Code, body)
	}
	historyPath := "/api/v1/network/quality/" + created.Data.ID + "/history?node_id=" + fixture.nodeID
	response = networkRequest(t, fixture.router, networkRequestSpec{method: http.MethodGet, path: historyPath})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"average_latency_ms":1.25`) {
		t.Fatalf("history status = %d, body = %s", response.Code, response.Body.String())
	}

	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodDelete, path: "/api/v1/network/tasks/" + created.Data.ID, want: http.StatusUnauthorized,
	})
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodDelete, path: "/api/v1/network/tasks/" + created.Data.ID,
		token: "admin-secret", want: http.StatusNoContent,
	})
}

func TestNetworkRoutesRejectInvalidResultsAndRanges(t *testing.T) {
	fixture := setupNetworkRouter(t)
	badResult := "{\"node_name\":\"route-node\",\"results\":[]}"
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodPost, path: "/api/v1/network/results", body: badResult,
		token: "agent-secret", want: http.StatusBadRequest,
	})
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/quality/missing/history", want: http.StatusBadRequest,
	})
	assertNetworkStatus(t, fixture.router, networkRequestSpec{
		method: http.MethodGet, path: "/api/v1/network/assignments?node_name=missing",
		token: "agent-secret", want: http.StatusUnauthorized,
	})
}

func assertNetworkStatus(
	t *testing.T,
	router *Router,
	spec networkRequestSpec,
) {
	t.Helper()
	response := networkRequest(t, router, spec)
	if response.Code != spec.want {
		t.Fatalf("%s %s status = %d, want %d, body = %s", spec.method, spec.path,
			response.Code, spec.want, response.Body.String())
	}
}

func networkRequest(
	t *testing.T,
	router *Router,
	spec networkRequestSpec,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(spec.method, spec.path, strings.NewReader(spec.body))
	request.Header.Set("Content-Type", "application/json")
	if spec.token != "" {
		request.Header.Set("Authorization", "Bearer "+spec.token)
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}
