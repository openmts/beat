package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/observability"
)

// TestInitializeRuntimeDiagnosticsFailures 覆盖私有诊断目录不可创建与就绪探针路径。
func TestInitializeRuntimeDiagnosticsFailures(t *testing.T) {
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "runtime-diagnostics"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&serverServices{}).initializeRuntimeDiagnostics(blocked); err == nil {
		t.Fatal("blocked runtime diagnostics directory was accepted")
	}
	services := &serverServices{}
	if err := services.initializeRuntimeDiagnostics(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if services.runtimeDiagnostics == nil {
		t.Fatal("runtime diagnostics service was not registered")
	}
	if err := services.runtimeDiagnostics.Health(); err != nil {
		t.Fatalf("runtime diagnostics health = %v", err)
	}
	services.closeRuntimeDiagnostics()
	(&serverServices{}).closeRuntimeDiagnostics()
}

// TestRuntimeDiagnosticsRouterAndMetrics 覆盖路由开关与 Prometheus 观测接线。
func TestRuntimeDiagnosticsRouterAndMetrics(t *testing.T) {
	if option := runtimeDiagnosticsRouterOption(nil); option == nil {
		t.Fatal("nil services returned no router option")
	}
	if option := runtimeDiagnosticsRouterOption(&serverServices{}); option == nil {
		t.Fatal("missing diagnostics service returned no router option")
	}
	runtimeDiagnosticsMetrics(nil, nil)
	service := &serverServices{}
	if err := service.initializeRuntimeDiagnostics(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.closeRuntimeDiagnostics)
	registry := observability.NewRegistry()
	runtimeDiagnosticsMetrics(registry, service.runtimeDiagnostics)
	output := registry.Prometheus()
	if output == "" {
		t.Fatal("runtime diagnostics metrics rendered nothing")
	}
	if option := runtimeDiagnosticsRouterOption(service); option == nil {
		t.Fatal("enabled diagnostics service returned no router option")
	}
}

// TestRuntimeDiagnosticsAudit 覆盖捕获审计在缺少主体与带主体两种输入下的行为。
func TestRuntimeDiagnosticsAudit(t *testing.T) {
	if runtimeDiagnosticsAudit(nil) != nil {
		t.Fatal("nil security service produced an audit hook")
	}
	f := newCommercialFixture(t)
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: f.stores.admins, Secrets: f.stores.secrets})
	if err != nil {
		t.Fatal(err)
	}
	audit := runtimeDiagnosticsAudit(security)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime-diagnostics/captures", nil)
	audit(request, "runtime_capture.start", "capture-1")
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "audit-admin", Username: "audit-admin"},
		Session: model.AdminSession{ID: "audit-session"}}
	ctx := middleware.WithAdminPrincipal(request.Context(), principal)
	audit(request.WithContext(ctx), "runtime_capture.start", "capture-2")
}
