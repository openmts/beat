package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestManagedNodeHandlerLifecycle(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	handler := NewNodeHandler(nodes, mtsStore)
	create := managedNodeRequestForTest(handler.HandleCreateManagedNode, http.MethodPost, "/nodes",
		`{"name":"managed","alias":"Primary","host":"127.0.0.1","port":22,
		"server_url":"https://beat.example","report_interval":"10s"}`, "")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var envelope struct {
		Data nodeCredentialResponse `json:"data"`
	}
	create = managedNodeRequestForTest(handler.HandleCreateManagedNode, http.MethodPost, "/nodes",
		`{"name":"second","host":"127.0.0.2","port":22,"server_url":"https://beat.example"}`, "")
	if err := json.NewDecoder(create.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode credential response: %v", err)
	}
	nodeID := envelope.Data.Node.ID
	if nodeID == "" || envelope.Data.AgentToken == "" ||
		envelope.Data.AgentConfig.ReportInterval != defaultAgentReportInterval {
		t.Fatalf("credential response = %#v", envelope.Data)
	}

	list := managedNodeRequestForTest(handler.HandleListManagedNodes, http.MethodGet, "/nodes/manage", "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "agent_credential_status") {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	rotate := managedNodeRequestForTest(handler.HandleRotateAgentToken, http.MethodPost, "/rotate",
		`{"server_url":"https://beat.example"}`, nodeID)
	if rotate.Code != http.StatusOK || !strings.Contains(rotate.Body.String(), "agent_token") {
		t.Fatalf("rotate status = %d, body = %s", rotate.Code, rotate.Body.String())
	}
	revoke := managedNodeRequestForTest(handler.HandleRevokeAgentToken, http.MethodPost, "/revoke", "", nodeID)
	if revoke.Code != http.StatusOK || !strings.Contains(revoke.Body.String(), "revoked") {
		t.Fatalf("revoke status = %d, body = %s", revoke.Code, revoke.Body.String())
	}
	install := managedNodeRequestForTest(handler.HandleAgentInstallConfig, http.MethodGet,
		"/install?server_url=https%3A%2F%2Fbeat.example", "", nodeID)
	if install.Code != http.StatusOK || strings.Contains(install.Body.String(), "beat_agent_v1_") {
		t.Fatalf("install status = %d, body = %s", install.Code, install.Body.String())
	}
}

func TestManagedNodeHandlerValidationAndNotFound(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	handler := NewNodeHandler(store.NewNodeStore(sqliteStore.DB), mtsStore)
	tests := []struct {
		name   string
		handle http.HandlerFunc
		method string
		path   string
		body   string
		id     string
		want   int
	}{
		{name: "invalid create JSON", handle: handler.HandleCreateManagedNode, method: http.MethodPost, body: "{", want: http.StatusBadRequest},
		{name: "invalid create fields", handle: handler.HandleCreateManagedNode, method: http.MethodPost, body: `{"name":"","host":"","port":0,"server_url":"bad"}`, want: http.StatusBadRequest},
		{name: "rotate missing", handle: handler.HandleRotateAgentToken, method: http.MethodPost, body: `{"server_url":"https://beat.example"}`, id: "missing", want: http.StatusNotFound},
		{name: "revoke missing", handle: handler.HandleRevokeAgentToken, method: http.MethodPost, id: "missing", want: http.StatusNotFound},
		{name: "install missing", handle: handler.HandleAgentInstallConfig, method: http.MethodGet, path: "/?server_url=https%3A%2F%2Fbeat.example", id: "missing", want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := managedNodeRequestForTest(
				test.handle, test.method, test.path, test.body, test.id,
			)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}

	createBody := `{"name":"duplicate","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	if response := managedNodeRequestForTest(
		handler.HandleCreateManagedNode, http.MethodPost, "/", createBody, "",
	); response.Code != http.StatusCreated {
		t.Fatalf("create duplicate fixture: %d", response.Code)
	}
	if response := managedNodeRequestForTest(
		handler.HandleCreateManagedNode, http.MethodPost, "/", createBody, "",
	); response.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body = %s", response.Code, response.Body.String())
	}
	node, err := store.NewNodeStore(sqliteStore.DB).GetNodeByName(t.Context(), "duplicate")
	if err != nil || node == nil {
		t.Fatalf("get duplicate fixture: node=%#v err=%v", node, err)
	}
	for _, body := range []string{"{", `{"server_url":"bad"}`} {
		response := managedNodeRequestForTest(
			handler.HandleRotateAgentToken, http.MethodPost, "/", body, node.ID,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid rotate status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	response := managedNodeRequestForTest(
		handler.HandleAgentInstallConfig, http.MethodGet, "/", "", node.ID,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid install status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestManagedNodeHandlerMetricErrors(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "metrics-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := sqliteStore.DB.Exec(
		"UPDATE nodes SET traffic_limit_type = 'invalid' WHERE id = ?", node.ID,
	); err != nil {
		t.Fatalf("invalidate traffic policy: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore)
	list := managedNodeRequestForTest(handler.HandleListManagedNodes, http.MethodGet, "/", "", "")
	if list.Code != http.StatusInternalServerError {
		t.Fatalf("list metric error status = %d", list.Code)
	}
	revoke := managedNodeRequestForTest(
		handler.HandleRevokeAgentToken, http.MethodPost, "/", "", node.ID,
	)
	if revoke.Code != http.StatusInternalServerError {
		t.Fatalf("revoke metric error status = %d", revoke.Code)
	}
}

func TestManagedNodeHandlerDatabaseErrors(t *testing.T) {
	stores := closedHandlerStores(t)
	handler := NewNodeHandler(stores.nodes, nil)
	tests := []struct {
		handle http.HandlerFunc
		method string
		body   string
	}{
		{handle: handler.HandleListManagedNodes, method: http.MethodGet},
		{handle: handler.HandleCreateManagedNode, method: http.MethodPost,
			body: `{"name":"node","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`},
		{handle: handler.HandleRotateAgentToken, method: http.MethodPost,
			body: `{"server_url":"https://beat.example"}`},
		{handle: handler.HandleRevokeAgentToken, method: http.MethodPost},
		{handle: handler.HandleAgentInstallConfig, method: http.MethodGet},
	}
	for _, test := range tests {
		response := managedNodeRequestForTest(test.handle, test.method, "/", test.body, "id")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("database error status = %d, body = %s", response.Code, response.Body.String())
		}
	}
}

func TestManagedNodeValidationHelpers(t *testing.T) {
	valid := managedNodeRequest{
		Name: "node", Host: "127.0.0.1", Port: 22,
		ServerURL: "https://beat.example", ReportInterval: "1s",
	}
	if !validManagedNodeRequest(valid) {
		t.Fatal("valid managed node rejected")
	}
	for _, mutate := range []func(*managedNodeRequest){
		func(value *managedNodeRequest) { value.Name = " " },
		func(value *managedNodeRequest) { value.Host = "" },
		func(value *managedNodeRequest) { value.Port = 65536 },
		func(value *managedNodeRequest) { value.ServerURL = "ftp://beat.example" },
		func(value *managedNodeRequest) { value.ReportInterval = "bad" },
		func(value *managedNodeRequest) { value.ReportInterval = "500ms" },
	} {
		value := valid
		mutate(&value)
		if validManagedNodeRequest(value) {
			t.Fatalf("invalid managed node accepted: %#v", value)
		}
	}
	if !validServerURL("http://[::1]:9180") || validServerURL("/relative") {
		t.Fatal("server URL validation mismatch")
	}
}

func TestWriteNodeCredentialHandlesInvalidTrafficPolicy(t *testing.T) {
	handler := NewNodeHandler(nil, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	handler.writeNodeCredential(response, request, http.StatusCreated, model.Node{
		TrafficLimitType: "invalid", TrafficResetDay: 1,
	}, "token", managedNodeRequest{ServerURL: "https://beat.example"})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func managedNodeRequestForTest(
	handle http.HandlerFunc,
	method string,
	path string,
	body string,
	nodeID string,
) *httptest.ResponseRecorder {
	if path == "" {
		path = "/"
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if nodeID != "" {
		request.SetPathValue("id", nodeID)
	}
	response := httptest.NewRecorder()
	handle(response, request)
	return response
}
