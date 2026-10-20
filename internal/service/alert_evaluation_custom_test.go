package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func evaluationCustomFixture(t *testing.T) (*evaluationRuntimeFixture, model.CustomMetric) {
	t.Helper()
	f := newEvaluationRuntimeFixture(t)
	metric, err := f.evaluator.custom.Create(t.Context(), model.CustomMetric{Name: "load", ShellCommand: "printf 90", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("INSERT INTO custom_metric_nodes(metric_id,node_id) VALUES(?,?)", metric.ID, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE alert_rules SET metric='custom_load',duration=60,window_seconds=10,aggregation='avg'"); err != nil {
		t.Fatal(err)
	}
	if err := f.metrics.AttachMetricCatalog(t.Context(), store.NewMetricCatalogStore(f.db.DB)); err != nil {
		t.Fatal(err)
	}
	return f, *metric
}

func customExecution(metric model.CustomMetric, at time.Time) model.CollectedCustomSamples {
	return model.CollectedCustomSamples{Values: map[string]float64{"load": 90}, Samples: map[string]model.CustomSampleMetadata{"load": {SampleID: uuid.NewString(), CapturedAt: at, CadenceMS: 5000, DefinitionVersion: model.CustomMetricDefinitionVersion(metric)}}}
}

func TestEvaluationCustomWindowsUseCompletionAndIgnoreNewerReportReplay(t *testing.T) {
	f, metric := evaluationCustomFixture(t)
	for range 2 {
		execution := customExecution(metric, f.at.Add(-time.Second))
		f.report(t, 20, execution)
		f.at = f.at.Add(5 * time.Second)
	}
	execution := customExecution(metric, f.at.Add(-time.Second))
	f.report(t, 20, execution)
	f.run(t)
	first := f.state(t)
	if first.Phase != model.AlertPhasePending || first.LastEvidenceAt == nil || !first.LastEvidenceAt.Equal(execution.Samples["load"].CapturedAt) {
		t.Fatal(first)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	f.report(t, 20, execution)
	f.run(t)
	replay := f.state(t)
	if replay.EvidenceID != first.EvidenceID || replay.TriggerElapsedMS != 0 || replay.ActiveEventID != "" {
		t.Fatal("host receipt refreshed custom execution", replay)
	}
	if replay.Status != model.EvaluationNoData || replay.Reason != "stale_value" {
		t.Fatal("stale execution hidden by fresh host", replay)
	}
}

func TestEvaluationCustomRejectsOldDefinitionAndMissingBinding(t *testing.T) {
	f, metric := evaluationCustomFixture(t)
	execution := customExecution(metric, f.at)
	if _, err := f.db.DB.Exec("UPDATE alert_rules SET window_seconds=0,aggregation='last'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE custom_metrics SET shell_command='printf 10' WHERE id=?", metric.ID); err != nil {
		t.Fatal(err)
	}
	f.report(t, 20, execution)
	f.run(t)
	if state := f.state(t); state.Status != model.EvaluationNoData || state.EvidenceID != "" {
		t.Fatal(state)
	}
	f.at = f.at.Add(alerteval.EvaluationInterval)
	if _, err := f.db.DB.Exec("DELETE FROM custom_metric_nodes WHERE metric_id=?", metric.ID); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	if state := f.state(t); state.Status != model.EvaluationOutOfScope || state.Reason != "custom_unbound" {
		t.Fatal(state)
	}
}
