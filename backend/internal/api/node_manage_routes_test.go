package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeManagementLifecycle(t *testing.T) {
	router := setupAuthenticatedRouter(t)
	createBody := `{"name":"managed","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	create := routerRequest(t, router, http.MethodPost, "/api/v1/nodes", createBody, "admin-secret")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data struct {
			Node struct {
				ID                    string `json:"id"`
				AgentCredentialStatus string `json:"agent_credential_status"`
			} `json:"node"`
			AgentToken string         `json:"agent_token"`
			Config     map[string]any `json:"agent_config"`
		} `json:"data"`
	}
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.Node.ID == "" || created.Data.Node.AgentCredentialStatus != "active" ||
		!strings.HasPrefix(created.Data.AgentToken, "beat_agent_v1_") ||
		created.Data.Config["agent_token"] != created.Data.AgentToken {
		t.Fatalf("created response = %#v", created.Data)
	}
	spoofedReport := routerRequest(t, router, http.MethodPost, "/api/v1/nodes/report",
		`{"name":"node","host":"10.0.0.9","port":2222}`, created.Data.AgentToken)
	if spoofedReport.Code != http.StatusCreated {
		t.Fatalf("per-node report status = %d, body = %s", spoofedReport.Code, spoofedReport.Body.String())
	}
	managedNode := routerRequest(t, router, http.MethodGet,
		"/api/v1/nodes/"+created.Data.Node.ID, "", "")
	if !strings.Contains(managedNode.Body.String(), `"name":"managed"`) ||
		!strings.Contains(managedNode.Body.String(), `"host":"10.0.0.9"`) {
		t.Fatalf("authenticated node binding failed: %s", managedNode.Body.String())
	}
	legacyAttempt := routerRequest(t, router, http.MethodPost, "/api/v1/nodes/report",
		`{"name":"managed","host":"10.0.0.10","port":22}`, "agent-secret")
	if legacyAttempt.Code != http.StatusUnauthorized {
		t.Fatalf("legacy token accessed managed node: %d", legacyAttempt.Code)
	}

	public := routerRequest(t, router, http.MethodGet, "/api/v1/nodes", "", "")
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), "agent_token") ||
		strings.Contains(public.Body.String(), "agent_credential") {
		t.Fatalf("public nodes leaked credentials: %s", public.Body.String())
	}
	managed := routerRequest(t, router, http.MethodGet, "/api/v1/nodes/manage", "", "admin-secret")
	if managed.Code != http.StatusOK || !strings.Contains(managed.Body.String(), "agent_token_prefix") ||
		strings.Contains(managed.Body.String(), created.Data.AgentToken) {
		t.Fatalf("managed list status = %d, body = %s", managed.Code, managed.Body.String())
	}

	rotate := routerRequest(t, router, http.MethodPost,
		"/api/v1/nodes/"+created.Data.Node.ID+"/token/rotate",
		`{"server_url":"https://beat.example"}`, "admin-secret")
	if rotate.Code != http.StatusOK || strings.Contains(rotate.Body.String(), created.Data.AgentToken) {
		t.Fatalf("rotate status = %d, body = %s", rotate.Code, rotate.Body.String())
	}
	revoke := routerRequest(t, router, http.MethodPost,
		"/api/v1/nodes/"+created.Data.Node.ID+"/token/revoke", "", "admin-secret")
	if revoke.Code != http.StatusOK ||
		!strings.Contains(revoke.Body.String(), `"agent_credential_status":"revoked"`) {
		t.Fatalf("revoke status = %d, body = %s", revoke.Code, revoke.Body.String())
	}
	install := routerRequest(t, router, http.MethodGet,
		"/api/v1/nodes/"+created.Data.Node.ID+"/install?server_url=https%3A%2F%2Fbeat.example",
		"", "admin-secret")
	if install.Code != http.StatusOK || strings.Contains(install.Body.String(), "beat_agent_v1_") ||
		strings.Contains(install.Body.String(), `"agent_token":`) {
		t.Fatalf("install status = %d, body = %s", install.Code, install.Body.String())
	}
}

func TestNodeManagementRequiresAdminAndRejectsDuplicateName(t *testing.T) {
	router := setupAuthenticatedRouter(t)
	body := `{"name":"node","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	unauthorized := routerRequest(t, router, http.MethodPost, "/api/v1/nodes", body, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized create status = %d", unauthorized.Code)
	}
	conflict := routerRequest(t, router, http.MethodPost, "/api/v1/nodes", body, "admin-secret")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
}

func routerRequest(
	t *testing.T,
	router *Router,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}
