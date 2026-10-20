package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"time"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/diagnostics"
	"github.com/beat/backend/internal/store"
)

const diagnosticMaintenanceInterval = 15 * time.Second

// initializeDiagnostics 构造诊断控制面；部署开关默认关闭。
func (services *serverServices) initializeDiagnostics(stores *serverStores) error {
	if stores == nil || stores.sqlite == nil || stores.sqlite.DB == nil || stores.secrets == nil || stores.mts == nil {
		return fmt.Errorf("diagnostic dependencies are unavailable")
	}
	enabled := false
	if value := os.Getenv("BEAT_DIAGNOSTICS_ENABLED"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("BEAT_DIAGNOSTICS_ENABLED must be a boolean")
		}
		enabled = parsed
	}
	service, err := diagnostics.NewService(diagnostics.Config{Database: stores.sqlite.DB, Secrets: stores.secrets,
		Metrics: stores.mts, Labels: diagnosticNodeLabels{nodes: stores.nodes}, Resolve: resolveDiagnosticHost, Enabled: enabled})
	if err != nil {
		return fmt.Errorf("initialize diagnostics: %w", err)
	}
	services.diagnostics = service
	return nil
}

// diagnosticNodeLabels 只提供显示名称；管理员界面从不展示原始节点 ID。
type diagnosticNodeLabels struct{ nodes *store.NodeStore }

func (labels diagnosticNodeLabels) NodeName(ctx context.Context, id string) (string, error) {
	if labels.nodes == nil {
		return "", fmt.Errorf("node store is unavailable")
	}
	node, err := labels.nodes.GetNode(ctx, id)
	if err != nil {
		return "", err
	}
	if node == nil {
		return "", fmt.Errorf("node %s is unknown", id)
	}
	return node.Name, nil
}

// resolveDiagnosticHost 在目标审批时解析 DNS 目标，解析结果全部逐条放行。
func resolveDiagnosticHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func diagnosticRouterOption(services *serverServices) api.RouterOption {
	if services == nil || services.diagnostics == nil {
		return func(*api.Router) {}
	}
	return api.WithDiagnostics(services.diagnostics)
}

func startDiagnosticWorker(config backgroundConfig, tasks *backgroundTasks) {
	if config.services == nil || config.services.diagnostics == nil || !config.services.diagnostics.Enabled() {
		return
	}
	worker := config.runtime.workers.register("diagnostics", diagnosticMaintenanceInterval)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{interval: diagnosticMaintenanceInterval,
			run: config.services.diagnostics.Reconcile, observe: worker.observe()})
	})
}
