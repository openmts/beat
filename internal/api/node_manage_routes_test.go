package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/testutil"
)

func TestNodeManagementLifecycle(t *testing.T) {
	router := setupSessionRouterWithOptions(t, WithAgentAuthentication("agent-secret"), reportMetricsFixtureOption(t))
	cookie := bootstrapTerminalOwner(t, router)
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal("reauthentication failed", reauth.Code)
	}
	createBody := `{"name":"managed","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	create := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes", createBody, cookie, "http://example.com")
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
		testutil.VersionedAgentReport(t, `{"name":"node","host":"10.0.0.9","port":2222,"metrics":{}}`), created.Data.AgentToken)
	if spoofedReport.Code != http.StatusCreated {
		t.Fatalf("per-node report status = %d, body = %s", spoofedReport.Code, spoofedReport.Body.String())
	}
	managedNode := routerRequest(t, router, http.MethodGet,
		"/api/v1/nodes/"+created.Data.Node.ID, "", "")
	// 公共节点视图按站点展示模式渲染地址，全新实例的基线值是 hidden，
	// 因此这里只断言名称；host 绑定在下一条管理端列表响应上验证。
	if !strings.Contains(managedNode.Body.String(), `"name":"managed"`) ||
		!strings.Contains(managedNode.Body.String(), `"host":""`) {
		t.Fatalf("public node view leaked or lost the address: %s", managedNode.Body.String())
	}
	legacyAttempt := routerRequest(t, router, http.MethodPost, "/api/v1/nodes/report",
		testutil.VersionedAgentReport(t, `{"name":"managed","host":"10.0.0.10","port":22,"metrics":{}}`), "agent-secret")
	if legacyAttempt.Code != http.StatusUnauthorized {
		t.Fatalf("legacy token accessed managed node: %d", legacyAttempt.Code)
	}

	public := routerRequest(t, router, http.MethodGet, "/api/v1/nodes", "", "")
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), "agent_token") ||
		strings.Contains(public.Body.String(), "agent_credential") {
		t.Fatalf("public nodes leaked credentials: %s", public.Body.String())
	}
	managed := requestRouterWithCookie(router, http.MethodGet, "/api/v1/nodes/manage", "", cookie, "http://example.com")
	if managed.Code != http.StatusOK || !strings.Contains(managed.Body.String(), "agent_token_prefix") ||
		!strings.Contains(managed.Body.String(), `"host":"10.0.0.9"`) ||
		strings.Contains(managed.Body.String(), created.Data.AgentToken) {
		t.Fatalf("managed list status = %d, body = %s", managed.Code, managed.Body.String())
	}

	rotate := requestRouterWithCookie(router, http.MethodPost,
		"/api/v1/nodes/"+created.Data.Node.ID+"/token/rotate",
		`{"server_url":"https://beat.example"}`, cookie, "http://example.com")
	if rotate.Code != http.StatusOK || strings.Contains(rotate.Body.String(), created.Data.AgentToken) {
		t.Fatalf("rotate status = %d, body = %s", rotate.Code, rotate.Body.String())
	}
	revoke := requestRouterWithCookie(router, http.MethodPost,
		"/api/v1/nodes/"+created.Data.Node.ID+"/token/revoke", "", cookie, "http://example.com")
	if revoke.Code != http.StatusOK ||
		!strings.Contains(revoke.Body.String(), `"agent_credential_status":"revoked"`) {
		t.Fatalf("revoke status = %d, body = %s", revoke.Code, revoke.Body.String())
	}
	install := requestRouterWithCookie(router, http.MethodGet,
		"/api/v1/nodes/"+created.Data.Node.ID+"/install?server_url=https%3A%2F%2Fbeat.example",
		"", cookie, "http://example.com")
	if install.Code != http.StatusOK || strings.Contains(install.Body.String(), "beat_agent_v1_") ||
		strings.Contains(install.Body.String(), `"agent_token":`) {
		t.Fatalf("install status = %d, body = %s", install.Code, install.Body.String())
	}
}

func TestNodeManagementRequiresAdminAndRejectsDuplicateName(t *testing.T) {
	router := setupSessionRouterWithOptions(t, WithAgentAuthentication("agent-secret"), reportMetricsFixtureOption(t))
	cookie := bootstrapTerminalOwner(t, router)
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal("reauthentication failed", reauth.Code)
	}
	body := `{"name":"node","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	unauthorized := routerRequest(t, router, http.MethodPost, "/api/v1/nodes", body, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized create status = %d", unauthorized.Code)
	}
	if _, err := router.nodeStore.UpsertNode(t.Context(), "node", "127.0.0.1", 22); err != nil {
		t.Fatal(err)
	}
	conflict := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes", body, cookie, "http://example.com")
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
	// 有界 JSON 解码契约要求非空载荷声明媒体类型，测试请求与真实客户端保持一致。
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}
