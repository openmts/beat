package alerter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestEvaluateRulesMarksStaleNodesOffline(t *testing.T) {
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	nodes := &mockNodeStore{}
	a := New(
		&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{},
		&mockMTSStore{}, nodes,
	)
	a.now = func() time.Time { return now }
	a.offlineAfter = 90 * time.Second

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("evaluate rules: %v", err)
	}
	if nodes.markCalls != 1 || !nodes.cutoff.Equal(now.Add(-90*time.Second)) {
		t.Fatalf("mark calls = %d, cutoff = %v", nodes.markCalls, nodes.cutoff)
	}
}

func TestEvaluateRulesReturnsStaleNodeUpdateError(t *testing.T) {
	want := errors.New("database unavailable")
	a := New(
		&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{},
		&mockMTSStore{}, &mockNodeStore{markErr: want},
	)
	if err := a.evaluateRules(t.Context()); !errors.Is(err, want) {
		t.Fatalf("evaluate error = %v, want %v", err, want)
	}
}

func TestHeartbeatAgeClampsFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	a := New(
		&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{},
		&mockMTSStore{}, &mockNodeStore{},
	)
	a.now = func() time.Time { return now }
	value, found, err := a.heartbeatAgeSeconds(&model.Node{LastSeen: now.Add(time.Minute)})
	if err != nil || !found || value != 0 {
		t.Fatalf("heartbeat age = %v, found = %v, error = %v", value, found, err)
	}
}

func TestEvaluateAvailabilityTriggersAfterDebounce(t *testing.T) {
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	node := model.Node{
		ID: "node-1", Name: "edge", Status: model.NodeStatusOffline,
		LastSeen: now.Add(-2 * time.Minute),
	}
	rule := model.AlertRule{
		ID: "rule-1", Name: "Node offline", Metric: model.MetricHeartbeatAgeSeconds,
		Operator: "gt", Threshold: 90, Duration: 30, Severity: model.AlertSeverityCritical,
	}
	events := &mockAlertEventStore{}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, events,
		&mockAlertChannelStore{}, &mockMTSStore{}, &mockNodeStore{nodes: []model.Node{node}},
	)
	a.now = func() time.Time { return now }

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("start availability debounce: %v", err)
	}
	if event, _ := events.GetActiveEvent(t.Context(), rule.ID, node.ID); event != nil {
		t.Fatalf("unexpected early event: %#v", event)
	}
	now = now.Add(31 * time.Second)
	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("finish availability debounce: %v", err)
	}
	event, err := events.GetActiveEvent(t.Context(), rule.ID, node.ID)
	if err != nil || event == nil {
		t.Fatalf("availability event = %#v, error = %v", event, err)
	}
	if !strings.Contains(event.Message, "edge") || !strings.Contains(event.Message, "offline") {
		t.Fatalf("availability message = %q", event.Message)
	}
}

func TestEvaluateAvailabilitySkipsNeverSeenNodes(t *testing.T) {
	rule := model.AlertRule{
		ID: "rule-1", Metric: model.MetricHeartbeatAgeSeconds,
		Operator: "gt", Threshold: 90, Duration: 0,
	}
	events := &mockAlertEventStore{}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, events,
		&mockAlertChannelStore{}, &mockMTSStore{},
		&mockNodeStore{nodes: []model.Node{{ID: "node-1", Status: model.NodeStatusOffline}}},
	)

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("evaluate never-seen node: %v", err)
	}
	if event, _ := events.GetActiveEvent(t.Context(), rule.ID, "node-1"); event != nil {
		t.Fatalf("unexpected never-seen event: %#v", event)
	}
}

func TestEvaluateAvailabilityResolvesPersistedEventAndNotifies(t *testing.T) {
	var delivered model.AlertEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode recovery payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	node := model.Node{
		ID: "node-1", Name: "edge", Status: model.NodeStatusOnline,
		LastSeen: now.Add(-5 * time.Second),
	}
	rule := model.AlertRule{
		ID: "rule-1", Name: "Node offline", Metric: model.MetricHeartbeatAgeSeconds,
		Operator: "gt", Threshold: 90, Duration: 30, Severity: model.AlertSeverityCritical,
	}
	events := &mockAlertEventStore{}
	if err := events.CreateEvent(t.Context(), &model.AlertEvent{
		ID: "event-1", RuleID: rule.ID, NodeID: node.ID, Message: "offline",
		Value: 120, Status: model.AlertStatusTriggered, TriggeredAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed active event: %v", err)
	}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, events,
		&mockAlertChannelStore{channels: []model.AlertChannel{{
			ID: "channel-1", ChannelType: "webhook", Config: server.URL, Enabled: true,
		}}}, &mockMTSStore{}, &mockNodeStore{nodes: []model.Node{node}},
	)
	a.now = func() time.Time { return now }

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("resolve availability event: %v", err)
	}
	if event, _ := events.GetActiveEvent(t.Context(), rule.ID, node.ID); event != nil {
		t.Fatalf("active event after recovery = %#v", event)
	}
	if delivered.Status != model.AlertStatusResolved || delivered.ResolvedAt == nil {
		t.Fatalf("recovery payload = %#v", delivered)
	}
	if !strings.Contains(delivered.Message, "recovered") {
		t.Fatalf("recovery message = %q", delivered.Message)
	}
}

func TestResolveAvailabilityReturnsChannelError(t *testing.T) {
	now := model.NowUTC()
	rule := &model.AlertRule{ID: "rule-1", Metric: model.MetricHeartbeatAgeSeconds}
	node := &model.Node{ID: "node-1", Name: "edge"}
	events := &mockAlertEventStore{}
	if err := events.CreateEvent(t.Context(), &model.AlertEvent{
		ID: "event-1", RuleID: rule.ID, NodeID: node.ID,
		Status: model.AlertStatusTriggered, TriggeredAt: now,
	}); err != nil {
		t.Fatalf("seed active event: %v", err)
	}
	want := errors.New("channel database unavailable")
	a := New(
		&mockAlertRuleStore{}, events, &mockAlertChannelStore{err: want},
		&mockMTSStore{}, &mockNodeStore{},
	)
	if err := a.resolveAlert(t.Context(), rule, node, 5); !errors.Is(err, want) {
		t.Fatalf("resolve error = %v, want %v", err, want)
	}
}

func TestEvaluateResourceRuleSkipsOfflineNode(t *testing.T) {
	rule := model.AlertRule{
		ID: "rule-1", Metric: "cpu", Operator: "gt", Threshold: 80,
	}
	mts := &mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 95}}}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, &mockAlertEventStore{},
		&mockAlertChannelStore{}, mts,
		&mockNodeStore{nodes: []model.Node{{ID: "node-1", Status: model.NodeStatusOffline}}},
	)

	if err := a.evaluateRules(context.Background()); err != nil {
		t.Fatalf("evaluate offline resource rule: %v", err)
	}
	if mts.latestQueries != 0 {
		t.Fatalf("MTS latest queries = %d, want 0", mts.latestQueries)
	}
}
