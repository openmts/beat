package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/ingress"
	"github.com/beat/backend/internal/model"
)

func TestLoopbackRequestHostRecognizesLocalHosts(t *testing.T) {
	cases := map[string]bool{
		"":                   false,
		"127.0.0.1:8081":     true,
		"127.0.0.1":          true,
		"[::1]:8081":         true,
		"[::1]":              true,
		"[::ffff:127.0.0.1]": true,
		"localhost":          true,
		"LocalHost:8081":     true,
		"example.test":       false,
		"192.168.1.10:8081":  false,
		"10.0.0.1":           false,
	}
	for host, want := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-enrollment/challenge", nil)
		request.Host = host
		if got := loopbackRequestHost(request); got != want {
			t.Fatalf("host %q loopback = %v, want %v", host, got, want)
		}
	}
}

func TestEnrollmentTransportRequiresSecureOrLoopbackTransport(t *testing.T) {
	router := &Router{}
	served := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusNoContent)
	})
	transport := router.enrollmentTransport(next)

	plain := httptest.NewRequest(http.MethodPost, "http://enroll.example.test/api/v1/agent-enrollment/challenge", nil)
	recorder := httptest.NewRecorder()
	transport.ServeHTTP(recorder, plain)
	if recorder.Code != http.StatusBadRequest || served != 0 {
		t.Fatalf("plaintext transport = %d, served = %d", recorder.Code, served)
	}

	loopback := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8081/api/v1/agent-enrollment/challenge", nil)
	recorder = httptest.NewRecorder()
	transport.ServeHTTP(recorder, loopback)
	if recorder.Code != http.StatusNoContent || served != 1 {
		t.Fatalf("loopback transport = %d, served = %d", recorder.Code, served)
	}

	secure := httptest.NewRequest(http.MethodPost, "https://enroll.example.test/api/v1/agent-enrollment/challenge", nil)
	secure.TLS = &tls.ConnectionState{}
	recorder = httptest.NewRecorder()
	transport.ServeHTTP(recorder, secure)
	if recorder.Code != http.StatusNoContent || served != 2 {
		t.Fatalf("secure transport = %d, served = %d", recorder.Code, served)
	}
}

func TestAgentDiscoveryOriginPrefersDeploymentBaseURL(t *testing.T) {
	config, err := deploymentaccess.Load(func(name string) string {
		if name == deploymentaccess.EnvPublicBaseURL {
			return "https://beat.example.test"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{}
	WithDeploymentAccess(config)(router)
	request := httptest.NewRequest(http.MethodPost, "https://internal.example.test/api/v1/agent-enrollment/challenge", nil)
	if origin := router.agentDiscoveryOrigin(request); origin != "https://beat.example.test" {
		t.Fatalf("canonical origin = %q", origin)
	}
	bare := &Router{}
	if origin := bare.agentDiscoveryOrigin(request); origin != "https://internal.example.test" {
		t.Fatalf("request origin = %q", origin)
	}
}

func TestAgentReportPassesThroughWithoutAgentAuth(t *testing.T) {
	router := &Router{}
	recorder := httptest.NewRecorder()
	served := false
	router.agentReport(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", nil))
	if !served || recorder.Code != http.StatusCreated {
		t.Fatalf("passthrough report = %d, served = %v", recorder.Code, served)
	}
}

func TestAgentIngestAdmissionBindsOneInFlightReportPerNode(t *testing.T) {
	limiter, err := ingress.New(ingress.DefaultCapacity())
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{ingressLimiter: limiter}
	identity := model.AgentIdentity{NodeID: "node-1", NodeName: "node-one", Mode: model.AgentCredentialActive}
	key := "node:" + middleware.Fingerprint(identity.NodeID)
	lease, rejection := limiter.Admit(ingress.AgentWrite, key)
	if rejection != nil {
		t.Fatalf("fixture admission = %v", rejection)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", nil).
		WithContext(middleware.WithAgentIdentity(t.Context(), identity))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	_, expected := limiter.Admit(ingress.AgentWrite, key)
	if expected == nil {
		t.Fatal("fixture did not exhaust the in-flight budget")
	}
	recorder := httptest.NewRecorder()
	router.agentIngestAdmission(next).ServeHTTP(recorder, request)
	if recorder.Code != expected.Status || recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("saturated admission = %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
	}

	lease.Release()
	recorder = httptest.NewRecorder()
	router.agentIngestAdmission(next).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("admitted report = %d", recorder.Code)
	}

	unknown := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", nil)
	recorder = httptest.NewRecorder()
	router.agentIngestAdmission(next).ServeHTTP(recorder, unknown)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unidentified report = %d", recorder.Code)
	}

	unlimited := &Router{}
	recorder = httptest.NewRecorder()
	unlimited.agentIngestAdmission(next).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unlimited admission = %d", recorder.Code)
	}
}
