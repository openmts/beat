package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type evaluationRuntimeFixture struct {
	evaluator *AlertEvaluator
	db        *store.SQLiteStore
	metrics   *store.MTSStore
	node      *model.Node
	rule      *model.AlertRule
	at        time.Time
}

func newEvaluationRuntimeFixture(t *testing.T) *evaluationRuntimeFixture {
	t.Helper()
	db, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "evaluation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	node, err := store.NewNodeStore(db.DB).UpsertNode(t.Context(), "evaluation", "192.0.2.10", 22)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := store.NewAlertRuleStore(db.DB).CreateAlertRule(t.Context(), &model.AlertRule{Name: "CPU", Metric: "cpu", Operator: "gt", Threshold: 80, Enabled: true, Severity: model.AlertSeverityWarning})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := store.NewMTSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metrics.Close() })
	secrets := testutil.NewSecrets(t, db.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: db.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	evaluator, err := NewAlertEvaluator(AlertEvaluatorConfig{Database: db.DB, Metrics: metrics, Queue: queue})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &evaluationRuntimeFixture{evaluator: evaluator, db: db, metrics: metrics, node: node, rule: rule, at: time.Now().UTC()}
	evaluator.now = func() time.Time { return fixture.at }
	return fixture
}

func (f *evaluationRuntimeFixture) report(t *testing.T, value float64, custom model.CollectedCustomSamples) {
	t.Helper()
	input := store.AgentReportInput{Identity: model.AgentIdentity{NodeID: f.node.ID, NodeName: f.node.Name, Mode: model.AgentCredentialActive, CredentialFingerprint: strings.Repeat("a", 64)}, Now: f.at,
		Report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: f.at, ReportIntervalMS: 1000, Name: f.node.Name, Host: "192.0.2.10", Port: 22, Metrics: &model.NodeMetrics{CPU: value}, CustomMetrics: custom.Values, CustomSamples: custom.Samples}}
	attempt, err := f.metrics.PrepareAgentReport(t.Context(), input, func(context.Context) (store.MetricReportSample, error) {
		values := make([]model.MetricValue, 0, len(custom.Values))
		for key, value := range custom.Values {
			values = append(values, model.MetricValue{Name: model.CustomMetricPrefix + key, Value: value})
		}
		return store.MetricReportSample{Custom: values}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (f *evaluationRuntimeFixture) state(t *testing.T) *model.AlertEvaluationState {
	t.Helper()
	state, err := f.evaluator.states.Get(t.Context(), store.AlertEvaluationKey{RuleID: f.rule.ID, NodeID: f.node.ID})
	if err != nil || state == nil {
		t.Fatal(state, err)
	}
	return state
}

func (f *evaluationRuntimeFixture) run(t *testing.T) {
	t.Helper()
	result, err := f.evaluator.RunOnce(t.Context())
	if err != nil || result.Completed != 1 || result.Failed != 0 {
		t.Fatal(result, err)
	}
}
