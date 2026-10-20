package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/iotest"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// operationsOnlyStub 只实现控制面接口，用于验证传输面注册的类型断言回落。
type operationsOnlyStub struct{ handler.RemoteOperations }

func TestRegisterRemoteTransferRoutesSkipsNonTransferService(t *testing.T) {
	router := &Router{}
	mux := http.NewServeMux()
	RegisterOperationRoutes(mux, OperationRouteConfig{Operations: operationsOnlyStub{}, ProtectAdmin: router.admin})
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/operations/status")
	if pattern := servedPattern(mux, http.MethodGet, "/api/v1/admin/operations/transfers"); pattern != "" {
		t.Fatalf("transfer routes must stay unregistered, got %q", pattern)
	}
}

func TestServiceTokenGuardRequiresRecentAuthentication(t *testing.T) {
	router := setupSessionRouter(t)
	recorder := httptest.NewRecorder()
	router.serviceTokenGuard(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "http://example.com/api/v1/admin/service-tokens", nil))
	if recorder.Code == http.StatusTeapot {
		t.Fatal("recent service token guard passed an unauthenticated request through")
	}
}

func TestSessionWebSocketAdminRejectsMissingCredentials(t *testing.T) {
	t.Run("setup required", func(t *testing.T) {
		router := setupSessionRouter(t)
		if status := probeSessionWebSocket(t, router); status != http.StatusUnauthorized {
			t.Fatalf("setup websocket status = %d", status)
		}
	})
	t.Run("bootstrapped", func(t *testing.T) {
		router := setupSessionRouter(t)
		bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
			`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
				`"password":"correct horse battery staple"}`, "", "http://example.com")
		if bootstrap.Code != http.StatusCreated {
			t.Fatalf("bootstrap status = %d", bootstrap.Code)
		}
		if status := probeSessionWebSocket(t, router); status != http.StatusUnauthorized {
			t.Fatalf("session websocket status = %d", status)
		}
	})
}

func probeSessionWebSocket(t *testing.T, router *Router) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.sessionWebSocketAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "http://example.com/api/v1/ws/terminal", nil))
	return recorder.Code
}

func TestMetricCatalogUpdateWithoutAdministratorSession(t *testing.T) {
	router := &Router{metricCatalog: &store.MetricCatalogStore{}}
	mux := http.NewServeMux()
	router.registerMetricCatalogRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPut, "http://example.com/api/v1/admin/metrics/catalog/cpu", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("metric catalog update status = %d", recorder.Code)
	}
}

func TestRuntimeDiagnosticsAuditPolicyResolvesCaptureDownload(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet,
		"http://example.com/api/v1/admin/runtime/captures/cap-1/download", nil)
	request.Pattern = "GET /api/v1/admin/runtime/captures/{id}/download"
	request.SetPathValue("id", "cap-1")
	action, kind, id := runtimeDiagnosticsAuditPolicy(request)
	if action != "runtime.capture.download" || kind != "runtime_capture" || id != "cap-1" {
		t.Fatalf("policy = %q/%q/%q", action, kind, id)
	}
}

func TestAtomicRouteAuditPolicyFallsBackToRuntimeDiagnostics(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete,
		"http://example.com/api/v1/admin/runtime/captures/cap-1", nil)
	request.Pattern = "DELETE /api/v1/admin/runtime/captures/{id}"
	request.SetPathValue("id", "cap-1")
	action, kind, id := atomicRouteAuditPolicy(request)
	if action != "runtime.capture.cancel" || kind != "runtime_capture" || id != "cap-1" {
		t.Fatalf("policy = %q/%q/%q", action, kind, id)
	}
}

func TestAdministratorAuditMarksSessionRevoke(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete, "http://example.com/api/v1/admin/sessions/sess-1", nil)
	request.Pattern = "DELETE /api/v1/admin/sessions/{id}"
	principal := &model.AdminPrincipal{}
	input := administratorRequestAudit(request, principal, requestAuditDecision{
		action: store.AdministratorSessionRevoke, resourceID: "sess-1", atomic: true, status: http.StatusOK,
	})
	if input.ResourceType != "session" {
		t.Fatalf("audit resource type = %q", input.ResourceType)
	}
}

func TestBodyNodeNameRejectsUnreadableBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/v1/nodes/report", nil)
	request.Body = io.NopCloser(iotest.ErrReader(errors.New("read failed")))
	if name, ok := bodyNodeName("node_name")(request); ok || name != "" {
		t.Fatalf("unreadable body resolved name %q", name)
	}
}
