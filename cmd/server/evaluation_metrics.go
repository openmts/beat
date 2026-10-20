package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

type evaluationSnapshot struct {
	result alerteval.RoundResult
	at     time.Time
}

func (state *runtimeState) recordEvaluation(result alerteval.RoundResult) {
	state.evaluation.Store(&evaluationSnapshot{result: result, at: time.Now().UTC()})
}

func (state *runtimeState) writeEvaluationMetrics(output *strings.Builder) {
	snapshot := state.evaluation.Load()
	if snapshot == nil {
		return
	}
	for _, metric := range []struct {
		name  string
		value float64
	}{
		{"beat_alert_evaluation_last_round_timestamp_seconds", float64(snapshot.at.Unix())},
		{"beat_alert_evaluation_round_admitted", float64(snapshot.result.Admitted)},
		{"beat_alert_evaluation_round_completed", float64(snapshot.result.Completed)},
		{"beat_alert_evaluation_round_failed", float64(snapshot.result.Failed)},
		{"beat_alert_evaluation_deferred", float64(snapshot.result.Deferred)},
		{"beat_alert_evaluation_oldest_due_age_seconds", snapshot.result.OldestDueAge.Seconds()},
		{"beat_alert_evaluation_backlog_complete", float64(boolMetric(snapshot.result.BacklogComplete))},
		{"beat_alert_evaluation_degraded", float64(boolMetric(snapshot.result.Degraded))},
	} {
		_, _ = fmt.Fprintf(output, "# TYPE %s gauge\n%s %g\n", metric.name, metric.name, metric.value)
	}
}
