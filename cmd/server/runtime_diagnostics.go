package main

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/runtimediag"
)

// initializeRuntimeDiagnostics 创建私有诊断目录；重启会先清空上次进程留下的产物。
func (services *serverServices) initializeRuntimeDiagnostics(dataDir string) error {
	var current *runtimediag.Service
	service, err := runtimediag.New(runtimediag.Config{
		Directory: filepath.Join(dataDir, "runtime-diagnostics"),
		Version:   buildinfo.Version,
		Commit:    buildinfo.Commit,
		Capture:   runtimediag.ProfileCapture,
		Readiness: func() error {
			if current == nil {
				return nil
			}
			return current.Health()
		},
	})
	if err != nil {
		return err
	}
	current = service
	services.runtimeDiagnostics = service
	return nil
}

// closeRuntimeDiagnostics 取消全部捕获并在退出前删除私有目录中的产物。
func (services *serverServices) closeRuntimeDiagnostics() {
	if services.runtimeDiagnostics == nil {
		return
	}
	if err := services.runtimeDiagnostics.Close(); err != nil {
		slog.Error("close runtime diagnostics failed", "reason", "cleanup_failed")
	}
}

func runtimeDiagnosticsRouterOption(services *serverServices) api.RouterOption {
	if services == nil || services.runtimeDiagnostics == nil {
		return func(*api.Router) {}
	}
	return api.WithRuntimeDiagnostics(services.runtimeDiagnostics, runtimeDiagnosticsAudit(services.security))
}

// runtimeDiagnosticsMetrics 把有界的捕获事件与占用快照暴露给 Prometheus。
func runtimeDiagnosticsMetrics(metrics *observability.Registry, service *runtimediag.Service) {
	if metrics == nil || service == nil {
		return
	}
	service.SetObserver(func(event runtimediag.Event) {
		metrics.ObserveRuntimeCaptureEvent(event.Kind, event.Result)
		if event.Result == runtimediag.EventRejected && event.Code != "" {
			metrics.ObserveRuntimeCaptureRejection(event.Code)
		}
	})
	metrics.SetRuntimeDiagnosticsStatistics(func() (observability.RuntimeDiagnosticsStatistics, error) {
		stats := service.Statistics()
		return observability.RuntimeDiagnosticsStatistics{Active: stats.Active, Bytes: stats.Bytes,
			CleanupFailures: stats.CleanupFailures}, nil
	})
}

// runtimeDiagnosticsAudit 记录固定的捕获审计动作；不写入 profile 内容或路径。
func runtimeDiagnosticsAudit(security *adminauth.Service) handler.RuntimeAudit {
	if security == nil {
		return nil
	}
	return func(request *http.Request, action, resourceID string) {
		principal, ok := middleware.AdminPrincipal(request.Context())
		if !ok {
			return
		}
		input := adminauth.AuditInput{RequestID: middleware.RequestID(request.Context()), Principal: &principal,
			Action: action, ResourceType: "runtime_capture", ResourceID: resourceID,
			Outcome: model.AuditOutcomeSuccess, IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}
		if err := security.RecordAudit(context.WithoutCancel(request.Context()), input); err != nil {
			slog.Error("runtime diagnostics audit write failed", "reason", "audit_write_failed")
		}
	}
}
