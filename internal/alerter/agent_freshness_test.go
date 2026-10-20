package alerter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	beatstore "github.com/beat/backend/internal/store"
)

type agentAlertSample struct {
	sample   beatstore.NodeMetricSample
	interval time.Duration
}

func writeFreshAgentSample(t *testing.T, metrics *beatstore.MTSStore, inputSample agentAlertSample) {
	sample, interval := inputSample.sample, inputSample.interval
	t.Helper()
	input := beatstore.AgentReportInput{Now: sample.Timestamp, Identity: model.AgentIdentity{NodeID: sample.NodeID, Mode: model.AgentCredentialActive, CredentialFingerprint: strings.Repeat("a", 64)},
		Report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: sample.Timestamp, ReportIntervalMS: interval.Milliseconds(), Host: "192.0.2.1", Port: 22, Metrics: &sample.Metrics}}
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatedReportIntervalControlsOfflineAndHeartbeatAlertGrace(t *testing.T) {
	sqlite, err := beatstore.NewSQLiteStore(filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	nodes := beatstore.NewNodeStore(sqlite.DB)
	node, err := nodes.UpsertNode(t.Context(), "interval-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := beatstore.NewMTSStore(filepath.Join(t.TempDir(), "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metrics.Close() })
	now := node.LastSeen
	writeFreshAgentSample(t, metrics, agentAlertSample{sample: beatstore.NodeMetricSample{NodeID: node.ID, Timestamp: now}, interval: 5 * time.Minute})
	a := newTestAlerter(&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{}, metrics, nodes)
	a.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := a.MaintainContacts(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := nodes.GetNode(t.Context(), node.ID)
	if err != nil || after.Status != model.NodeStatusOnline {
		t.Fatal("long report interval was incorrectly offline", err)
	}
	rule, err := a.freshAlertRule(t.Context(), &model.AlertRule{Metric: model.MetricHeartbeatAgeSeconds, Operator: "gt", Threshold: 90}, node)
	if err != nil || rule.Threshold != 900 {
		t.Fatal("heartbeat rule ignored authenticated cadence", err)
	}
	a.now = func() time.Time { return now.Add(16 * time.Minute) }
	if err := a.MaintainContacts(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err = nodes.GetNode(t.Context(), node.ID)
	if err != nil || after.Status != model.NodeStatusOffline {
		t.Fatal("expired long-interval contact remained online", err)
	}
}

type receiptAlertStore struct {
	mockMTSStore
	receipt    *model.AgentReceipt
	receiptErr error
}

func (store *receiptAlertStore) LatestAgentReceipt(context.Context, string) (*model.AgentReceipt, error) {
	return store.receipt, store.receiptErr
}

func TestAlertEvaluationDistinguishesStaleAndUnavailableTelemetry(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []time.Duration{time.Second, 40 * time.Second, 2 * time.Minute, -time.Second} {
		metrics := &receiptAlertStore{receipt: &model.AgentReceipt{CapturedAt: now.Add(-age), ReportIntervalMS: 5000}}
		a := newTestAlerter(&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{}, metrics, &mockNodeStore{})
		a.now = func() time.Time { return now }
		_, err := a.freshAlertRule(t.Context(), &model.AlertRule{Metric: "cpu"}, &model.Node{ID: "node"})
		if age > 30*time.Second && !errors.Is(err, ErrTelemetryUnavailable) {
			t.Fatal("stale metric evaluated", age, err)
		}
		if age <= 30*time.Second && err != nil {
			t.Fatal("current metric rejected", err)
		}
	}
	metrics := &receiptAlertStore{}
	a := newTestAlerter(&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{}, metrics, &mockNodeStore{})
	if _, err := a.freshAlertRule(t.Context(), &model.AlertRule{Metric: "cpu"}, &model.Node{ID: "node"}); !errors.Is(err, ErrTelemetryUnavailable) {
		t.Fatal("missing receipt became healthy")
	}
	metrics.receiptErr = errors.New("storage failure")
	if _, err := a.freshAlertRule(t.Context(), &model.AlertRule{Metric: "cpu"}, &model.Node{ID: "node"}); !errors.Is(err, metrics.receiptErr) {
		t.Fatal("query failure disappeared")
	}
	if _, err := a.freshAlertRule(t.Context(), &model.AlertRule{Metric: model.MetricHeartbeatAgeSeconds}, &model.Node{ID: "node"}); !errors.Is(err, metrics.receiptErr) {
		t.Fatal("contact cadence query failure disappeared")
	}
}
