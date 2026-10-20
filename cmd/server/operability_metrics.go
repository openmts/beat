package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (operations *operability) handleMetrics(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if _, err := w.Write([]byte(operations.prometheus(request.Context()))); err != nil {
		slog.ErrorContext(request.Context(), "write metrics response", "error", err)
	}
}

func (operations *operability) prometheus(ctx context.Context) string {
	var output strings.Builder
	_, _ = output.WriteString(operations.metrics.Prometheus())
	_, _ = fmt.Fprintf(&output, "# TYPE beat_build_info gauge\nbeat_build_info{version=%s,commit=%s} 1\n",
		strconv.Quote(buildinfo.Version), strconv.Quote(buildinfo.Commit))
	operations.writeReadinessMetrics(&output, ctx)
	operations.writeNodeMetrics(&output, ctx)
	operations.writeStorageMetrics(&output)
	operations.runtime.writeEvaluationMetrics(&output)
	operations.runtime.writeFanoutMetrics(&output)
	operations.runtime.writeWorkerMetrics(&output)
	operations.writeServiceMetrics(&output, ctx)
	operations.writeSecurityMetrics(&output, ctx)
	if ingestion, ok := operations.mts.(interface{ AgentIngestPrometheus() string }); ok {
		_, _ = output.WriteString(ingestion.AgentIngestPrometheus())
	}
	return output.String()
}

func (operations *operability) writeReadinessMetrics(output *strings.Builder, ctx context.Context) {
	response, _ := operations.readiness(ctx)
	writeAgentRolloutMetrics(output, response.Checks["agent_rollouts"])
	_, _ = output.WriteString("# TYPE beat_readiness_check gauge\n")
	names := []string{"sqlite", "secrets", "notifications", "mts", "restore", "accelerators", "agent_rollouts"}
	if _, ok := response.Checks["login_security"]; ok {
		names = append(names, "login_security")
	}
	for _, worker := range fixedWorkerNames {
		if _, ok := response.Checks["worker_"+worker]; ok {
			names = append(names, "worker_"+worker)
		}
	}
	for _, name := range names {
		value := 0
		if response.Checks[name].Status == "ok" {
			value = 1
		}
		_, _ = fmt.Fprintf(output, "beat_readiness_check{check=%q} %d\n", name, value)
	}
	restorePending := response.Checks["restore"].Detail == "restart_required"
	_, _ = fmt.Fprintf(output, "# TYPE beat_restore_pending gauge\nbeat_restore_pending %d\n", boolMetric(restorePending))

	workers := operations.runtime.schedulerWorkers()
	if len(workers) > 0 {
		_, _ = output.WriteString("# TYPE beat_scheduler_worker_running gauge\n")
		_, _ = output.WriteString("# TYPE beat_scheduler_worker_last_success_unix_seconds gauge\n")
		_, _ = output.WriteString("# TYPE beat_scheduler_worker_consecutive_failures gauge\n")
		_, _ = output.WriteString("# TYPE beat_scheduler_worker_restarts_total counter\n")
		for _, worker := range workers {
			if !knownWorker(worker.Name) {
				continue
			}
			lastSuccess := int64(0)
			if !worker.LastSuccess.IsZero() {
				lastSuccess = worker.LastSuccess.Unix()
			}
			_, _ = fmt.Fprintf(output, "beat_scheduler_worker_running{worker=%q} %d\n",
				worker.Name, boolMetric(worker.Running))
			_, _ = fmt.Fprintf(output, "beat_scheduler_worker_last_success_unix_seconds{worker=%q} %d\n",
				worker.Name, lastSuccess)
			_, _ = fmt.Fprintf(output, "beat_scheduler_worker_consecutive_failures{worker=%q} %d\n",
				worker.Name, worker.ConsecutiveFails)
			_, _ = fmt.Fprintf(output, "beat_scheduler_worker_restarts_total{worker=%q} %d\n",
				worker.Name, worker.Restarts)
		}
	}
}

func (operations *operability) writeNodeMetrics(output *strings.Builder, ctx context.Context) {
	nodes, err := operations.nodes.ListNodes(ctx, "")
	if err != nil {
		_, _ = output.WriteString("# TYPE beat_agents_query_success gauge\nbeat_agents_query_success 0\n")
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
	_, _ = output.WriteString("# TYPE beat_agents_query_success gauge\nbeat_agents_query_success 1\n")
	_, _ = fmt.Fprintf(output, "# TYPE beat_agents_total gauge\nbeat_agents_total %d\n", len(nodes))
	_, _ = fmt.Fprintf(output, "# TYPE beat_agents_online gauge\nbeat_agents_online %d\n", online)
	_, _ = fmt.Fprintf(output, "# TYPE beat_agent_max_heartbeat_age_seconds gauge\nbeat_agent_max_heartbeat_age_seconds %g\n", maximumAge)
}

func (operations *operability) writeStorageMetrics(output *strings.Builder) {
	sqliteBytes, sqliteErr := operations.sqlite.DiskUsage()
	mtsBytes, mtsErr := operations.mts.DiskUsage()
	_, _ = output.WriteString("# TYPE beat_storage_bytes gauge\n")
	_, _ = fmt.Fprintf(output, "beat_storage_bytes{store=\"sqlite\"} %d\n", metricValue(sqliteBytes, sqliteErr))
	_, _ = fmt.Fprintf(output, "beat_storage_bytes{store=\"mts\"} %d\n", metricValue(mtsBytes, mtsErr))
}

func (operations *operability) writeServiceMetrics(output *strings.Builder, ctx context.Context) {
	operations.writeNotificationQueueMetrics(output, ctx)
	operations.writeMetricErasureMetrics(output, ctx)
	operations.writeAcceleratorMetrics(output, ctx)
	_, _ = fmt.Fprintf(output, "# TYPE beat_backup_running gauge\nbeat_backup_running %d\n", boolMetric(operations.backups.Running()))
	_, _ = fmt.Fprintf(output, "# TYPE beat_maintenance_running gauge\nbeat_maintenance_running %d\n",
		boolMetric(operations.maintenance.Running()))
	stats := operations.notifications.Stats()
	_, _ = output.WriteString("# TYPE beat_notification_deliveries_total counter\n")
	_, _ = fmt.Fprintf(output, "beat_notification_deliveries_total{result=\"success\"} %d\n", stats.Success)
	_, _ = fmt.Fprintf(output, "beat_notification_deliveries_total{result=\"failed\"} %d\n", stats.Failed)
	if operations.terminals != nil {
		_, _ = output.WriteString("# TYPE beat_terminal_sessions_active gauge\n")
		_, _ = fmt.Fprintf(output, "beat_terminal_sessions_active %d\n", operations.terminals.ActiveSessions())
	}
	operations.writeBackupRecordMetrics(output, ctx)
}

var backupMetricStates = []string{
	model.BackupStateRunning, model.BackupStateReady, model.BackupStateFailed,
	model.BackupStateValidated, model.BackupStateStaging, model.BackupStateStaged,
	model.BackupStateInterrupted, model.BackupStateMissing, model.BackupStateDeleting,
}

func (operations *operability) writeBackupRecordMetrics(output *strings.Builder, ctx context.Context) {
	counts, err := operations.backupStateCounts(ctx)
	if err != nil {
		return
	}
	_, _ = output.WriteString("# TYPE beat_backups_total gauge\n")
	for _, state := range backupMetricStates {
		_, _ = fmt.Fprintf(output, "beat_backups_total{state=%q} %d\n", state, counts[state])
	}
	if quarantined, quarantineErr := operations.backupRecords.CountQuarantine(ctx); quarantineErr == nil {
		_, _ = output.WriteString("# TYPE beat_backups_quarantined gauge\n")
		_, _ = fmt.Fprintf(output, "beat_backups_quarantined %d\n", quarantined)
	}
	operations.writeBackupReconcileMetrics(output)
}

// backupStateCounts 用固定页大小走完备份历史，避免一次整表下发。
func (operations *operability) backupStateCounts(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{}
	cursor := ""
	for {
		page, err := operations.backupRecords.ListBackupsPage(ctx,
			store.BackupQuery{Cursor: cursor, Limit: store.BackupMaximumPage})
		if err != nil {
			return nil, err
		}
		for _, record := range page.Items {
			counts[record.State]++
		}
		if page.NextCursor == "" {
			return counts, nil
		}
		cursor = page.NextCursor
	}
}

// writeBackupReconcileMetrics 暴露启动对账的分类结果，不包含任何路径、文件名或主体。
func (operations *operability) writeBackupReconcileMetrics(output *strings.Builder) {
	outcome := operations.backupReconcile
	entries := []struct {
		name  string
		value int
	}{
		{"recovered", outcome.Recovered}, {"interrupted", outcome.Interrupted},
		{"missing", outcome.Missing}, {"rolled_back", outcome.RolledBack},
		{"deleted", outcome.Deleted}, {"expired", outcome.Expired},
		{"quarantined", outcome.Quarantined}, {"partial_removed", outcome.PartialRemoved},
		{"journals_removed", outcome.JournalsRemoved}, {"deferred", outcome.Deferred},
	}
	_, _ = output.WriteString("# TYPE beat_backup_reconcile_outcomes gauge\n")
	for _, entry := range entries {
		_, _ = fmt.Fprintf(output, "beat_backup_reconcile_outcomes{outcome=%q} %d\n", entry.name, entry.value)
	}
}
