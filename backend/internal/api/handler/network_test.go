package handler

import (
	"encoding/json"
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

type networkHandlerFixture struct {
	handler *NetworkHandler
	tasks   *store.NetworkTaskStore
	sqlite  *store.SQLiteStore
	mts     *store.MTSStore
	node    *model.Node
	now     time.Time
}

func setupNetworkHandler(t *testing.T) networkHandlerFixture {
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
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "handler-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	tasks := store.NewNetworkTaskStore(sqliteStore.DB)
	handler := NewNetworkHandler(tasks, nodes, mtsStore)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }
	return networkHandlerFixture{
		handler: handler, tasks: tasks, sqlite: sqliteStore, mts: mtsStore, node: node, now: now,
	}
}

func TestNetworkHandlerWorkflow(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)

	assertHandlerStatus(t, fixture.handler.HandleListNetworkTasks,
		networkHandlerRequest(http.MethodGet, "/api/v1/network/tasks", ""), http.StatusOK)
	assertHandlerStatus(t, fixture.handler.HandlePublicNetworkQuality,
		networkHandlerRequest(http.MethodGet, "/api/v1/network/quality", ""), http.StatusOK)
	assertHandlerStatus(t, fixture.handler.HandleNetworkAssignments,
		networkAgentRequest(fixture, http.MethodGet, "/api/v1/network/assignments", ""), http.StatusOK)

	updateBody := validTaskBody(false, true, true, `,"node_ids":["`+fixture.node.ID+`"]`)
	updateRequest := networkHandlerRequest(http.MethodPut, "/api/v1/network/tasks/"+created.ID, updateBody)
	updateRequest.SetPathValue("task_id", created.ID)
	assertHandlerStatus(t, fixture.handler.HandleUpdateNetworkTask, updateRequest, http.StatusOK)

	sortRequest := networkHandlerRequest(http.MethodPut, "/api/v1/network/tasks/sort", `{"ids":["`+created.ID+`"]}`)
	assertHandlerStatus(t, fixture.handler.HandleSortNetworkTasks, sortRequest, http.StatusOK)
	postNetworkResult(t, fixture, created.ID, fixture.now)

	publicHistory := historyRequest(created.ID, fixture.node.ID, "")
	assertHandlerStatus(t, fixture.handler.HandlePublicNetworkHistory, publicHistory, http.StatusOK)
	adminHistory := historyRequest(created.ID, fixture.node.ID, "")
	assertHandlerStatus(t, fixture.handler.HandleAdminNetworkHistory, adminHistory, http.StatusOK)

	deleteRequest := networkHandlerRequest(http.MethodDelete, "/api/v1/network/tasks/"+created.ID, "")
	deleteRequest.SetPathValue("task_id", created.ID)
	assertHandlerStatus(t, fixture.handler.HandleDeleteNetworkTask, deleteRequest, http.StatusNoContent)
}

func TestPublicNetworkViewsExcludeHiddenNodes(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)
	isPublic := false
	if _, err := store.NewNodeStore(fixture.sqlite.DB).UpdateNode(
		t.Context(), fixture.node.ID, store.NodeUpdate{
			Alias: fixture.node.Alias, GroupID: fixture.node.GroupID,
			SSHPublicKey: fixture.node.SSHPublicKey, IsPublic: &isPublic,
		},
	); err != nil {
		t.Fatalf("hide node: %v", err)
	}

	publicQuality := httptest.NewRecorder()
	fixture.handler.HandlePublicNetworkQuality(publicQuality,
		networkHandlerRequest(http.MethodGet, "/api/v1/network/quality", ""))
	if publicQuality.Code != http.StatusOK || strings.Contains(publicQuality.Body.String(), fixture.node.ID) {
		t.Fatalf("public quality status = %d, body = %s", publicQuality.Code, publicQuality.Body.String())
	}
	assertHandlerStatus(t, fixture.handler.HandlePublicNetworkHistory,
		historyRequest(created.ID, fixture.node.ID, ""), http.StatusNotFound)

	adminTasks := httptest.NewRecorder()
	fixture.handler.HandleListNetworkTasks(adminTasks,
		networkHandlerRequest(http.MethodGet, "/api/v1/network/tasks", ""))
	if adminTasks.Code != http.StatusOK || !strings.Contains(adminTasks.Body.String(), fixture.node.ID) {
		t.Fatalf("admin tasks status = %d, body = %s", adminTasks.Code, adminTasks.Body.String())
	}
}

func TestNetworkHandlerRejectsInvalidRequests(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)
	tests := []struct {
		name    string
		handle  http.HandlerFunc
		request *http.Request
		status  int
	}{
		{name: "create body", handle: fixture.handler.HandleCreateNetworkTask, request: networkHandlerRequest(http.MethodPost, "/tasks", "{}"), status: http.StatusBadRequest},
		{name: "update body", handle: fixture.handler.HandleUpdateNetworkTask, request: requestWithTaskID(http.MethodPut, "missing", "{"), status: http.StatusBadRequest},
		{name: "update missing", handle: fixture.handler.HandleUpdateNetworkTask, request: requestWithTaskID(http.MethodPut, "missing", validTaskBody(true, true, true, "")), status: http.StatusNotFound},
		{name: "delete missing", handle: fixture.handler.HandleDeleteNetworkTask, request: requestWithTaskID(http.MethodDelete, "missing", ""), status: http.StatusNotFound},
		{name: "sort empty", handle: fixture.handler.HandleSortNetworkTasks, request: networkHandlerRequest(http.MethodPut, "/sort", "{}"), status: http.StatusBadRequest},
		{name: "sort missing", handle: fixture.handler.HandleSortNetworkTasks, request: networkHandlerRequest(http.MethodPut, "/sort", `{"ids":["missing"]}`), status: http.StatusBadRequest},
		{name: "assignment identity", handle: fixture.handler.HandleNetworkAssignments, request: networkHandlerRequest(http.MethodGet, "/assignments", ""), status: http.StatusUnauthorized},
		{name: "empty results", handle: fixture.handler.HandleNetworkResults, request: networkAgentRequest(fixture, http.MethodPost, "/results", `{"node_name":"handler-node","results":[]}`), status: http.StatusBadRequest},
		{name: "history node", handle: fixture.handler.HandlePublicNetworkHistory, request: requestWithTaskID(http.MethodGet, created.ID, ""), status: http.StatusBadRequest},
		{name: "history task", handle: fixture.handler.HandlePublicNetworkHistory, request: historyRequest("missing", fixture.node.ID, ""), status: http.StatusNotFound},
		{name: "history range", handle: fixture.handler.HandlePublicNetworkHistory, request: historyRequest(created.ID, fixture.node.ID, "?from=bad&to=bad"), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertHandlerStatus(t, test.handle, test.request, test.status)
		})
	}

	postInvalidNetworkResult(t, fixture, created.ID, fixture.now.Add(-10*time.Minute), http.StatusBadRequest)
	disabled := createHandlerTask(t, fixture, false, false)
	postInvalidNetworkResult(t, fixture, disabled.ID, fixture.now, http.StatusConflict)
	assertHandlerStatus(t, fixture.handler.HandlePublicNetworkHistory,
		historyRequest(disabled.ID, fixture.node.ID, ""), http.StatusNotFound)
	assertHandlerStatus(t, fixture.handler.HandlePublicNetworkHistory,
		historyRequest(created.ID, "missing-node", ""), http.StatusNotFound)
}

func TestNetworkHandlerStoreFailures(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		fixture := setupNetworkHandler(t)
		created := createHandlerTask(t, fixture, true, true)
		if err := fixture.sqlite.Close(); err != nil {
			t.Fatalf("close SQLite: %v", err)
		}
		assertHandlerStatus(t, fixture.handler.HandleListNetworkTasks,
			networkHandlerRequest(http.MethodGet, "/tasks", ""), http.StatusInternalServerError)
		assertHandlerStatus(t, fixture.handler.HandlePublicNetworkQuality,
			networkHandlerRequest(http.MethodGet, "/quality", ""), http.StatusInternalServerError)
		assertHandlerStatus(t, fixture.handler.HandleNetworkAssignments,
			networkAgentRequest(fixture, http.MethodGet, "/assignments", ""), http.StatusInternalServerError)
		assertHandlerStatus(t, fixture.handler.HandleDeleteNetworkTask,
			requestWithTaskID(http.MethodDelete, created.ID, ""), http.StatusInternalServerError)
		assertHandlerStatus(t, fixture.handler.HandleAdminNetworkHistory,
			historyRequest(created.ID, fixture.node.ID, ""), http.StatusInternalServerError)
		postInvalidNetworkResult(t, fixture, created.ID, fixture.now, http.StatusInternalServerError)
	})

	t.Run("mts", func(t *testing.T) {
		fixture := setupNetworkHandler(t)
		created := createHandlerTask(t, fixture, true, true)
		if err := fixture.mts.Close(); err != nil {
			t.Fatalf("close MTS: %v", err)
		}
		assertHandlerStatus(t, fixture.handler.HandlePublicNetworkQuality,
			networkHandlerRequest(http.MethodGet, "/quality", ""), http.StatusOK)
		postInvalidNetworkResult(t, fixture, created.ID, fixture.now, http.StatusInternalServerError)
		assertHandlerStatus(t, fixture.handler.HandleAdminNetworkHistory,
			historyRequest(created.ID, fixture.node.ID, ""), http.StatusOK)
		assertHandlerStatus(t, fixture.handler.HandleDeleteNetworkTask,
			requestWithTaskID(http.MethodDelete, created.ID, ""), http.StatusNoContent)
	})
}

func TestParseNetworkRangeBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "missing to", query: "?from=2026-07-29T11:00:00Z", wantErr: true},
		{name: "invalid to", query: "?from=2026-07-29T11:00:00Z&to=bad", wantErr: true},
		{name: "reverse", query: "?from=2026-07-29T12:00:00Z&to=2026-07-29T11:00:00Z", wantErr: true},
		{name: "too long", query: "?from=2026-06-01T00:00:00Z&to=2026-07-29T12:00:00Z", wantErr: true},
		{name: "valid", query: "?from=2026-07-29T10:00:00%2B02:00&to=2026-07-29T11:00:00%2B02:00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := networkHandlerRequest(http.MethodGet, "/history"+test.query, "")
			from, to, err := parseNetworkRange(request, now)
			if (err != nil) != test.wantErr {
				t.Fatalf("range error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && (from.Location() != time.UTC || to.Location() != time.UTC) {
				t.Fatalf("range locations = %v, %v", from.Location(), to.Location())
			}
		})
	}
}

func createHandlerTask(t *testing.T, fixture networkHandlerFixture, enabled, public bool) model.NetworkTask {
	t.Helper()
	request := networkHandlerRequest(http.MethodPost, "/tasks", validTaskBody(true, enabled, public, ""))
	response := httptest.NewRecorder()
	fixture.handler.HandleCreateNetworkTask(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create task status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Data model.NetworkTask `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	return body.Data
}

func postNetworkResult(t *testing.T, fixture networkHandlerFixture, taskID string, finished time.Time) {
	t.Helper()
	postInvalidNetworkResult(t, fixture, taskID, finished, http.StatusNoContent)
}

func postInvalidNetworkResult(
	t *testing.T,
	fixture networkHandlerFixture,
	taskID string,
	finished time.Time,
	want int,
) {
	t.Helper()
	body := `{"node_name":"handler-node","results":[{"task_id":"` + taskID +
		`","finished_at":"` + finished.Format(time.RFC3339Nano) +
		`","latency_ms":1.5,"success":true,"status_code":0,"error_code":"none"}]}`
	assertHandlerStatus(t, fixture.handler.HandleNetworkResults,
		networkAgentRequest(fixture, http.MethodPost, "/results", body), want)
}

func historyRequest(taskID, nodeID, suffix string) *http.Request {
	request := networkHandlerRequest(http.MethodGet, "/history"+suffix, "")
	request.SetPathValue("task_id", taskID)
	query := request.URL.Query()
	query.Set("node_id", nodeID)
	request.URL.RawQuery = query.Encode()
	return request
}

func requestWithTaskID(method, taskID, body string) *http.Request {
	request := networkHandlerRequest(method, "/tasks/"+taskID, body)
	request.SetPathValue("task_id", taskID)
	return request
}

func networkHandlerRequest(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, strings.NewReader(body))
}

func networkAgentRequest(
	fixture networkHandlerFixture,
	method string,
	path string,
	body string,
) *http.Request {
	request := networkHandlerRequest(method, path, body)
	identity := model.AgentIdentity{
		NodeID: fixture.node.ID, NodeName: fixture.node.Name, Mode: model.AgentCredentialActive,
	}
	return request.WithContext(middleware.WithAgentIdentity(request.Context(), identity))
}

func assertHandlerStatus(t *testing.T, handle http.HandlerFunc, request *http.Request, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	handle(response, request)
	if response.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, want, response.Body.String())
	}
}

func validTaskBody(allNodes, enabled, public bool, extra string) string {
	return `{"name":"Probe","type":"icmp","target":"127.0.0.1","ip_family":"auto",` +
		`"interval_seconds":60,"timeout_milliseconds":1000,"all_nodes":` + boolText(allNodes) +
		`,"enabled":` + boolText(enabled) + `,"is_public":` + boolText(public) +
		`,"sort_order":0` + extra + `}`
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
