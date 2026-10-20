package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/ingress"
	"github.com/beat/backend/internal/metricsfanout"
)

type fanoutEvents struct {
	mu                                   sync.Mutex
	accepted, rejected, coalesced, stale int64
}

func (state *runtimeState) observeFanout(event metricsfanout.Event) {
	if event.Kind == metricsfanout.EventBuild {
		var err error
		if event.Reason != metricsfanout.ReasonNone {
			err = fmt.Errorf("snapshot build failed")
		}
		state.workers.observer("metrics_fanout")(err)
	}
	state.fanoutEvents.mu.Lock()
	defer state.fanoutEvents.mu.Unlock()
	switch event.Kind {
	case metricsfanout.EventUpgrade:
		if event.Reason == metricsfanout.ReasonNone {
			state.fanoutEvents.accepted += event.Count
		} else {
			state.fanoutEvents.rejected += event.Count
		}
	case metricsfanout.EventDrop:
		switch event.Reason {
		case metricsfanout.ReasonCoalesced:
			state.fanoutEvents.coalesced += event.Count
		case metricsfanout.ReasonStale:
			state.fanoutEvents.stale += event.Count
		}
	}
}

type fanoutView struct {
	Generation           uint64    `json:"generation"`
	Timestamp            time.Time `json:"timestamp"`
	AgeSeconds           float64   `json:"age_seconds"`
	Stale                bool      `json:"stale"`
	Available            bool      `json:"available"`
	Connections          int       `json:"connections"`
	Reserved             int       `json:"reserved"`
	TrackedIPs           int       `json:"tracked_ips"`
	Builds               uint64    `json:"builds"`
	Failures             uint64    `json:"failures"`
	Dropped              uint64    `json:"dropped"`
	BuildDurationSeconds float64   `json:"build_duration_seconds"`
	SkippedCadences      uint64    `json:"skipped_cadences"`
	Closed               bool      `json:"closed"`
}

func (state *runtimeState) fanoutSnapshot() fanoutView {
	service := state.fanout.Load()
	if service == nil {
		return fanoutView{}
	}
	value := service.Stats()
	return fanoutView{Generation: value.Generation.Generation, Timestamp: value.Generation.Timestamp, AgeSeconds: value.Generation.Age.Seconds(), Stale: value.Generation.Stale, Available: value.Generation.Available, Connections: value.Connections, Reserved: value.Reserved, TrackedIPs: value.TrackedIPs, Builds: value.Builds, Failures: value.Failures, Dropped: value.Dropped, BuildDurationSeconds: value.BuildDuration.Seconds(), SkippedCadences: value.SkippedCadences, Closed: value.Closed}
}

func (state *runtimeState) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	handler.JSONResponse(w, http.StatusOK, struct {
		ServerTime time.Time         `json:"server_time"`
		Stopping   bool              `json:"stopping"`
		Workers    []workerSnapshot  `json:"workers"`
		Metrics    fanoutView        `json:"metrics"`
		Ingress    *ingress.Snapshot `json:"ingress,omitempty"`
	}{time.Now().UTC(), state.stopping.Load(), state.schedulerWorkers(), state.fanoutSnapshot(), state.ingressSnapshot()})
}

// ingressSnapshot 让所有者只读地看到有效上限与当前占用，不暴露任何身份。
func (state *runtimeState) ingressSnapshot() *ingress.Snapshot {
	limiter := ingressLimiter(state)
	if limiter == nil {
		return nil
	}
	snapshot := limiter.Snapshot()
	return &snapshot
}

func (state *runtimeState) writeFanoutMetrics(output *strings.Builder) {
	value := state.fanoutSnapshot()
	for _, metric := range []struct {
		name  string
		value float64
	}{
		{"snapshot_build_duration_seconds", value.BuildDurationSeconds}, {"snapshot_age_seconds", value.AgeSeconds}, {"snapshot_scheduler_skipped_total", float64(value.SkippedCadences)}, {"ws_connections", float64(value.Connections)},
	} {
		_, _ = fmt.Fprintf(output, "beat_metrics_%s %g\n", metric.name, metric.value)
	}
	_, _ = fmt.Fprintf(output, "beat_metrics_snapshot_builds_total{result=\"success\"} %d\nbeat_metrics_snapshot_builds_total{result=\"failure\"} %d\n", value.Builds-value.Failures, value.Failures)
	state.fanoutEvents.mu.Lock()
	defer state.fanoutEvents.mu.Unlock()
	_, _ = fmt.Fprintf(output, "beat_metrics_ws_upgrades_total{result=\"accepted\"} %d\nbeat_metrics_ws_upgrades_total{result=\"rejected\"} %d\n", state.fanoutEvents.accepted, state.fanoutEvents.rejected)
	_, _ = fmt.Fprintf(output, "beat_metrics_ws_dropped_snapshots_total{reason=\"coalesced\"} %d\nbeat_metrics_ws_dropped_snapshots_total{reason=\"stale\"} %d\n", state.fanoutEvents.coalesced, state.fanoutEvents.stale)
}

func (state *runtimeState) beginShutdown() {
	state.stopping.Store(true)
	if state.workers != nil {
		state.workers.stopping.Store(true)
	}
}

func (state *runtimeState) publicBuilder(router *api.Router) func(context.Context) (metricsfanout.Snapshot, error) {
	return func(ctx context.Context) (metricsfanout.Snapshot, error) {
		value, err := router.BuildMetricsSnapshot(ctx)
		if errors.Is(err, handler.ErrPublicMetricQuery) && ctx.Err() == nil {
			state.mtsSnapshotFailures.Add(1)
		}
		return value, err
	}
}

func (state *runtimeState) writeWorkerMetrics(output *strings.Builder) {
	for _, worker := range state.schedulerWorkers() {
		if !knownWorker(worker.Name) {
			continue
		}
		age := float64(0)
		if !worker.LastSuccess.IsZero() {
			age = max(0, time.Since(worker.LastSuccess).Seconds())
		}
		_, _ = fmt.Fprintf(output, "beat_worker_up{worker=%q} %d\nbeat_worker_last_success_age_seconds{worker=%q} %g\n", worker.Name, boolMetric(worker.Running), worker.Name, age)
		_, _ = fmt.Fprintf(output, "beat_worker_consecutive_failures{worker=%q} %d\nbeat_worker_failures_total{worker=%q,class=\"iteration_failed\"} %d\n", worker.Name, worker.ConsecutiveFails, worker.Name, worker.Failures)
		_, _ = fmt.Fprintf(output, "beat_worker_scheduler_lag_seconds{worker=%q} %g\n", worker.Name, worker.SchedulerLagSeconds)
		for _, reason := range []string{"unexpected_exit", "panic"} {
			value := 0
			if worker.State == "failed" && worker.Failure == reason {
				value = 1
			}
			_, _ = fmt.Fprintf(output, "beat_worker_unexpected_exits_total{worker=%q,reason=%q} %d\n", worker.Name, reason, value)
		}
	}
	_, _ = fmt.Fprintf(output, "beat_mts_query_failures_total{consumer=\"metrics_fanout\",class=\"read_failed\"} %d\n", state.mtsSnapshotFailures.Load())
}
