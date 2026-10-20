package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/ingress"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/store"
)

type sqliteMonitor interface {
	Ready(context.Context) error
	DiskUsage() (int64, error)
}

type mtsMonitor interface {
	Health() (bool, []string)
	DiskUsage() (int64, error)
}

type nodeMonitor interface {
	ListNodes(context.Context, string) ([]model.Node, error)
}

type backupMonitor interface {
	ListBackupsPage(context.Context, store.BackupQuery) (model.BackupPage, error)
	CountQuarantine(context.Context) (int, error)
}

type runningMonitor interface {
	Running() bool
}

type notificationMonitor interface {
	Stats() notification.DeliveryStats
}

type terminalMonitor interface {
	ActiveSessions() int
}

type runtimeState struct {
	ingress             atomic.Pointer[ingress.Limiter]
	metrics             atomic.Pointer[observability.Registry]
	fanout              atomic.Pointer[metricsfanout.Service]
	stopping            atomic.Bool
	fanoutEvents        fanoutEvents
	mtsSnapshotFailures atomic.Int64
	evaluation          atomic.Pointer[evaluationSnapshot]
	workers             *workerSupervisor
}

func newRuntimeState() *runtimeState {
	return &runtimeState{workers: newWorkerSupervisor()}
}

func (state *runtimeState) schedulerWorkers() []workerSnapshot {
	if state.workers == nil {
		return nil
	}
	return state.workers.snapshot()
}

// schedulerHealth 返回各 worker 的健康原因；空字符串表示健康。
func (state *runtimeState) schedulerHealth(now time.Time) []workerHealth {
	if state.workers == nil {
		return nil
	}
	return state.workers.health(now)
}

type operability struct {
	agentRollouts     rolloutMonitor
	accelerators      acceleratorMonitor
	sqlite            sqliteMonitor
	secrets           secretReadiness
	mts               mtsMonitor
	nodes             nodeMonitor
	backupRecords     backupMonitor
	backupReconcile   backup.ReconcileOutcome
	backups           runningMonitor
	maintenance       runningMonitor
	notifications     notificationMonitor
	notificationQueue secretReadiness
	metricErasure     secretReadiness
	metricErasureJobs metricErasureStatusSource
	security          securityMetricsSource
	terminals         terminalMonitor
	metrics           *observability.Registry
	runtime           *runtimeState
	pendingRestore    func(string) (bool, error)
	dataDir           string
}

type readinessCheck struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type readinessResponse struct {
	Status  string                    `json:"status"`
	Version string                    `json:"version"`
	Checks  map[string]readinessCheck `json:"checks"`
}

func newOperability(
	config httpHandlerConfig,
	metrics *observability.Registry,
) *operability {
	return &operability{
		sqlite: config.stores.sqlite, mts: config.stores.mts, nodes: config.stores.nodes,
		secrets:       config.stores.secrets,
		accelerators:  acceleratorMonitoring(config.services),
		agentRollouts: rolloutMonitoring(config.services),
		backupRecords: config.stores.backups, backups: config.services.backups,
		backupReconcile: config.services.reconciliation,
		maintenance:     config.services.maintenance, notifications: config.services.delivery,
		notificationQueue: config.services.delivery,
		metricErasure:     config.services.metricErasure, metricErasureJobs: config.stores.metricErasure,
		security:  securityMetricsSourceFrom(config.services),
		terminals: config.services.terminal,
		metrics:   metrics, runtime: config.runtime, pendingRestore: backup.PendingRestore, dataDir: config.dataDir,
	}
}

func (operations *operability) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", operations.handleHealth)
	mux.HandleFunc("GET /readyz", operations.handleReady)
	mux.HandleFunc("GET /metrics", operations.handleMetrics)
}

func (operations *operability) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeStatusJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildinfo.Version})
}

func (operations *operability) handleReady(w http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	response, ready := operations.readiness(ctx)
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeStatusJSON(w, status, response)
}

func (operations *operability) readiness(ctx context.Context) (readinessResponse, bool) {
	checks := map[string]readinessCheck{
		"accelerators":   operations.acceleratorCheck(ctx),
		"agent_rollouts": operations.agentRolloutCheck(ctx),
		"sqlite":         operations.sqliteCheck(ctx),
		"secrets":        operations.secretsCheck(ctx),
		"notifications":  operations.notificationCheck(ctx),
		"mts":            operations.mtsCheck(),
		"restore":        operations.restoreCheck(),
		"metric_erasure": operations.metricErasureCheck(ctx),
	}
	if operations.security != nil {
		checks["login_security"] = operations.loginSecurityCheck(ctx)
	}
	operations.addWorkerChecks(checks)
	ready := !operations.runtime.stopping.Load()
	for name, check := range checks {
		if name != "restore" && check.Status != "ok" {
			ready = false
		}
	}
	status := "ready"
	if !ready {
		status = "not_ready"
	}
	return readinessResponse{Status: status, Version: buildinfo.Version, Checks: checks}, ready
}

func (operations *operability) sqliteCheck(ctx context.Context) readinessCheck {
	if err := operations.sqlite.Ready(ctx); err != nil {
		return readinessCheck{Status: "error", Detail: sqliteFailureDetail(err)}
	}
	return readinessCheck{Status: "ok"}
}

// sqliteFailureDetail 只给出粗粒度原因：运维能一眼区分磁盘写满、只读挂载与权限问题，
// 未认证的 /readyz 又不会拿到内部路径或 SQL 文本。
func sqliteFailureDetail(err error) string {
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "no space left on device"
	case errors.Is(err, syscall.EROFS):
		return "read-only file system"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission denied"
	default:
		return "unavailable"
	}
}

func (operations *operability) mtsCheck() readinessCheck {
	healthy, _ := operations.mts.Health()
	if !healthy {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) restoreCheck() readinessCheck {
	pending, err := operations.pendingRestore(operations.dataDir)
	if err != nil {
		return readinessCheck{Status: "error", Detail: "invalid"}
	}
	if pending {
		return readinessCheck{Status: "ok", Detail: "restart_required"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) schedulerCheck() readinessCheck {
	health := operations.runtime.schedulerHealth(time.Now().UTC())
	if len(health) == 0 {
		return readinessCheck{Status: "error", Detail: "stopped"}
	}
	unhealthy := make([]string, 0, len(health))
	for _, worker := range health {
		if worker.Reason != "" {
			unhealthy = append(unhealthy, worker.Name+" "+worker.Reason)
		}
	}
	if len(unhealthy) > 0 {
		return readinessCheck{Status: "error", Detail: strings.Join(unhealthy, ",")}
	}
	return readinessCheck{Status: "ok"}
}

func writeStatusJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write status response", "error", err)
	}
}

func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
}

func metricValue(value int64, err error) int64 {
	if err != nil {
		return 0
	}
	return value
}

var fixedWorkerNames = []string{"alerts", "traffic_reports", "maintenance", "metrics_fanout", "notifications", "metric_retention", "metric_erasure", "node_contacts", "remote_operations", "geoip", "node_lifecycle", "accelerators", "agent_rollouts", "agent_rollout_retention", "fleet_summaries", "application_gc"}

func (operations *operability) addWorkerChecks(checks map[string]readinessCheck) {
	for _, name := range fixedWorkerNames[:4] {
		checks["worker_"+name] = readinessCheck{Status: "error", Detail: "stopped"}
	}
	for _, worker := range operations.runtime.schedulerHealth(time.Now().UTC()) {
		if !knownWorker(worker.Name) {
			continue
		}
		check := readinessCheck{Status: "ok"}
		if worker.Reason != "" {
			check = readinessCheck{Status: "error", Detail: worker.Reason}
			if worker.Reason == "failed" {
				check.Detail = "unavailable"
			}
		}
		checks["worker_"+worker.Name] = check
	}
	if service := operations.runtime.fanout.Load(); service != nil {
		state := service.Stats()
		if !state.Generation.Available || state.Generation.Stale || state.Closed {
			checks["worker_metrics_fanout"] = readinessCheck{Status: "error", Detail: "stale"}
		}
	}
}

func knownWorker(name string) bool {
	for _, known := range fixedWorkerNames {
		if name == known {
			return true
		}
	}
	return false
}
