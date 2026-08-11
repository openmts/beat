package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/observability"
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
	List(context.Context) ([]model.BackupRecord, error)
}

type runningMonitor interface {
	Running() bool
}

type notificationMonitor interface {
	Stats() notification.DeliveryStats
}

type runtimeState struct {
	schedulers atomic.Bool
}

type operability struct {
	sqlite         sqliteMonitor
	mts            mtsMonitor
	nodes          nodeMonitor
	backupRecords  backupMonitor
	backups        runningMonitor
	maintenance    runningMonitor
	notifications  notificationMonitor
	metrics        *observability.Registry
	runtime        *runtimeState
	pendingRestore func(string) (bool, error)
	dataDir        string
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
		backupRecords: config.stores.backups, backups: config.services.backups,
		maintenance: config.services.maintenance, notifications: config.services.delivery,
		metrics: metrics, runtime: config.runtime, pendingRestore: backup.PendingRestore, dataDir: config.dataDir,
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
		"sqlite":     operations.sqliteCheck(ctx),
		"mts":        operations.mtsCheck(),
		"restore":    operations.restoreCheck(),
		"schedulers": operations.schedulerCheck(),
	}
	ready := true
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
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
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
	if !operations.runtime.schedulers.Load() {
		return readinessCheck{Status: "error", Detail: "stopped"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) handleMetrics(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if _, err := w.Write([]byte(operations.prometheus(request.Context()))); err != nil {
		slog.ErrorContext(request.Context(), "write metrics response", "error", err)
	}
}

func (operations *operability) prometheus(ctx context.Context) string {
	var output strings.Builder
	output.WriteString(operations.metrics.Prometheus())
	fmt.Fprintf(&output, "# TYPE beat_build_info gauge\nbeat_build_info{version=%s,commit=%s} 1\n",
		strconv.Quote(buildinfo.Version), strconv.Quote(buildinfo.Commit))
	operations.writeReadinessMetrics(&output, ctx)
	operations.writeNodeMetrics(&output, ctx)
	operations.writeStorageMetrics(&output)
	operations.writeServiceMetrics(&output, ctx)
	return output.String()
}

func (operations *operability) writeReadinessMetrics(output *strings.Builder, ctx context.Context) {
	response, _ := operations.readiness(ctx)
	output.WriteString("# TYPE beat_readiness_check gauge\n")
	for _, name := range []string{"sqlite", "mts", "restore", "schedulers"} {
		value := 0
		if response.Checks[name].Status == "ok" {
			value = 1
		}
		fmt.Fprintf(output, "beat_readiness_check{check=%q} %d\n", name, value)
	}
	restorePending := response.Checks["restore"].Detail == "restart_required"
	fmt.Fprintf(output, "# TYPE beat_restore_pending gauge\nbeat_restore_pending %d\n", boolMetric(restorePending))
}

func (operations *operability) writeNodeMetrics(output *strings.Builder, ctx context.Context) {
	nodes, err := operations.nodes.ListNodes(ctx, "")
	if err != nil {
		output.WriteString("# TYPE beat_agents_query_success gauge\nbeat_agents_query_success 0\n")
		return
	}
	online := 0
	maximumAge := 0.0
	now := time.Now().UTC()
	for _, node := range nodes {
		if node.Status == model.NodeStatusOnline {
			online++
		}
		if !node.LastSeen.IsZero() && now.After(node.LastSeen) && now.Sub(node.LastSeen).Seconds() > maximumAge {
			maximumAge = now.Sub(node.LastSeen).Seconds()
		}
	}
	output.WriteString("# TYPE beat_agents_query_success gauge\nbeat_agents_query_success 1\n")
	fmt.Fprintf(output, "# TYPE beat_agents_total gauge\nbeat_agents_total %d\n", len(nodes))
	fmt.Fprintf(output, "# TYPE beat_agents_online gauge\nbeat_agents_online %d\n", online)
	fmt.Fprintf(output, "# TYPE beat_agent_max_heartbeat_age_seconds gauge\nbeat_agent_max_heartbeat_age_seconds %g\n", maximumAge)
}

func (operations *operability) writeStorageMetrics(output *strings.Builder) {
	sqliteBytes, sqliteErr := operations.sqlite.DiskUsage()
	mtsBytes, mtsErr := operations.mts.DiskUsage()
	output.WriteString("# TYPE beat_storage_bytes gauge\n")
	fmt.Fprintf(output, "beat_storage_bytes{store=\"sqlite\"} %d\n", metricValue(sqliteBytes, sqliteErr))
	fmt.Fprintf(output, "beat_storage_bytes{store=\"mts\"} %d\n", metricValue(mtsBytes, mtsErr))
}

func (operations *operability) writeServiceMetrics(output *strings.Builder, ctx context.Context) {
	fmt.Fprintf(output, "# TYPE beat_backup_running gauge\nbeat_backup_running %d\n", boolMetric(operations.backups.Running()))
	fmt.Fprintf(output, "# TYPE beat_maintenance_running gauge\nbeat_maintenance_running %d\n",
		boolMetric(operations.maintenance.Running()))
	stats := operations.notifications.Stats()
	output.WriteString("# TYPE beat_notification_deliveries_total counter\n")
	fmt.Fprintf(output, "beat_notification_deliveries_total{result=\"success\"} %d\n", stats.Success)
	fmt.Fprintf(output, "beat_notification_deliveries_total{result=\"failed\"} %d\n", stats.Failed)
	operations.writeBackupRecordMetrics(output, ctx)
}

func (operations *operability) writeBackupRecordMetrics(output *strings.Builder, ctx context.Context) {
	records, err := operations.backupRecords.List(ctx)
	if err != nil {
		return
	}
	counts := map[string]int{}
	for _, record := range records {
		counts[record.State]++
	}
	output.WriteString("# TYPE beat_backups_total gauge\n")
	for _, state := range []string{
		model.BackupStateRunning, model.BackupStateReady, model.BackupStateFailed,
		model.BackupStateValidated, model.BackupStateStaged,
	} {
		fmt.Fprintf(output, "beat_backups_total{state=%q} %d\n", state, counts[state])
	}
}

func writeStatusJSON(w http.ResponseWriter, status int, value any) {
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
