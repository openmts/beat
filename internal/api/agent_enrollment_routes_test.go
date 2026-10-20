package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/testutil"
)

type enrollmentRouteRequest struct {
	path, body, token, origin, remote, forwarded string
	cookie                                       *http.Cookie
	secure                                       bool
}

func callEnrollmentRoute(router *Router, input enrollmentRouteRequest) *httptest.ResponseRecorder {
	scheme := "http"
	if input.secure {
		scheme = "https"
	}
	request := httptest.NewRequest(http.MethodPost, scheme+"://example.com"+input.path, strings.NewReader(input.body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", input.origin)
	request.Header.Set("X-Forwarded-For", input.forwarded)
	if input.cookie != nil {
		request.AddCookie(input.cookie)
	}
	if input.token != "" {
		request.Header.Set("Authorization", "Bearer "+input.token)
	}
	if input.remote != "" {
		request.RemoteAddr = input.remote
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}

func enrollmentOwnerCookie(t *testing.T, router *Router) *http.Cookie {
	t.Helper()
	response := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if response.Code != 201 {
		t.Fatalf("bootstrap status %d", response.Code)
	}
	return response.Result().Cookies()[0]
}

func TestEnrollmentRoutesRequireSessionAndSameOrigin(t *testing.T) {
	router := setupSessionRouter(t)
	path := "/api/v1/admin/agent-enrollment-tokens"
	body := `{"server_url":"https://example.com"}`
	bootstrapBearer := callEnrollmentRoute(router, enrollmentRouteRequest{path: path, body: body, token: "admin-secret"})
	if bootstrapBearer.Code != 401 {
		t.Fatalf("bootstrap authority issued enrollment: %d", bootstrapBearer.Code)
	}
	cookie := enrollmentOwnerCookie(t, router)
	for _, input := range []enrollmentRouteRequest{
		{path: path, body: body}, {path: path, body: body, token: "admin-secret"},
		{path: path, body: body, cookie: cookie, origin: "https://attacker.example"},
	} {
		response := callEnrollmentRoute(router, input)
		if response.Code != 401 && response.Code != 403 {
			t.Fatalf("unauthorized issuance status %d", response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("private route cache/CORS policy missing")
		}
	}
	response := callEnrollmentRoute(router, enrollmentRouteRequest{path: path, body: body, cookie: cookie, origin: "http://example.com"})
	if response.Code != 201 {
		t.Fatalf("authorized issuance status %d: %s", response.Code, response.Body.String())
	}
}

func TestEnrollmentRouteCreatesOneNodeAndAuthenticatesReports(t *testing.T) {
	router := setupSessionRouterWithOptions(t, WithAgentAuthentication("legacy-agent"), reportMetricsFixtureOption(t))
	cookie := enrollmentOwnerCookie(t, router)
	issued := callEnrollmentRoute(router, enrollmentRouteRequest{path: "/api/v1/admin/agent-enrollment-tokens",
		body: `{"server_url":"https://example.com"}`, cookie: cookie, origin: "http://example.com"})
	var credential agentjoin.Envelope[agentjoin.IssueResult]
	if err := json.Unmarshal(issued.Body.Bytes(), &credential); err != nil || credential.Code != 201 {
		t.Fatalf("issuance envelope: %v", err)
	}
	agentToken, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(agentjoin.Request{RequestID: "stable-id", AgentToken: agentToken.Plaintext, NodeName: "joined", SSHPort: 2222})
	if err != nil {
		t.Fatal(err)
	}
	input := enrollmentRouteRequest{path: "/api/v1/agents/join", body: string(body), token: credential.Data.Token, secure: true,
		remote: "[2001:db8::5]:1234", forwarded: "10.0.0.99"}
	joined := callEnrollmentRoute(router, input)
	var result agentjoin.Envelope[agentjoin.Result]
	if err := json.Unmarshal(joined.Body.Bytes(), &result); err != nil || result.Code != 200 {
		t.Fatalf("join status %d: %v", joined.Code, err)
	}
	if result.Data.Config.AdvertisedHost != "2001:db8::5" || result.Data.Config.SSHPort != 2222 {
		t.Fatal("untrusted forwarded host accepted")
	}
	retry := callEnrollmentRoute(router, input)
	if retry.Code != 200 || retry.Body.String() != joined.Body.String() {
		t.Fatal("retry changed node identity")
	}
	report := enrollmentRouteRequest{path: "/api/v1/nodes/report", body: testutil.VersionedAgentReport(t, `{"name":"spoofed","host":"2001:db8::5","port":2222,"metrics":{}}`), token: agentToken.Plaintext}
	if response := callEnrollmentRoute(router, report); response.Code != 201 {
		t.Fatalf("joined agent cannot report: %d", response.Code)
	}
	report.token = credential.Data.Token
	if response := callEnrollmentRoute(router, report); response.Code != 401 {
		t.Fatal("join credential authenticated a report")
	}
	input.token = agentToken.Plaintext
	if response := callEnrollmentRoute(router, input); response.Code != 401 {
		t.Fatal("permanent credential authenticated enrollment")
	}
	node, err := router.nodeStore.GetNode(t.Context(), result.Data.NodeID)
	if err != nil || node == nil || node.Name != "joined" || node.Status != "online" {
		t.Fatalf("node binding lost: %v", err)
	}
}

func TestEnrollmentHonorsTrustedProxyAddressAndTransport(t *testing.T) {
	router := setupSessionRouter(t)
	cookie := enrollmentOwnerCookie(t, router)
	issued := callEnrollmentRoute(router, enrollmentRouteRequest{path: "/api/v1/admin/agent-enrollment-tokens",
		body: `{"server_url":"https://example.com"}`, cookie: cookie, origin: "http://example.com"})
	var credential agentjoin.Envelope[agentjoin.IssueResult]
	if err := json.Unmarshal(issued.Body.Bytes(), &credential); err != nil {
		t.Fatal(err)
	}
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(agentjoin.Request{RequestID: "proxy-id", AgentToken: token.Plaintext, NodeName: "proxied", SSHPort: 22})
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := middleware.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://internal/api/v1/agents/join", strings.NewReader(string(body)))
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credential.Data.Token)
	request.Header.Set("X-Forwarded-For", "198.51.100.9")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	middleware.RequestContext(trusted)(router.ServeHandler()).ServeHTTP(response, request)
	var result agentjoin.Envelope[agentjoin.Result]
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 200 || result.Data.Config.AdvertisedHost != "198.51.100.9" {
		t.Fatalf("trusted proxy result status %d: %v", response.Code, err)
	}
}
