package alerter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestEvaluateRulesMarksStaleNodesOffline(t *testing.T) {
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	nodes := &mockNodeStore{}
	a := newTestAlerter(
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
	a := newTestAlerter(
		&mockAlertRuleStore{}, &mockAlertEventStore{}, &mockAlertChannelStore{},
		&mockMTSStore{}, &mockNodeStore{markErr: want},
	)
	if err := a.evaluateRules(t.Context()); !errors.Is(err, want) {
		t.Fatalf("evaluate error = %v, want %v", err, want)
	}
}

func TestHeartbeatAgeClampsFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	a := newTestAlerter(
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
	a := newTestAlerter(
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
	a := newTestAlerter(
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
	a := newTestAlerter(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, events,
		&mockAlertChannelStore{channels: []model.AlertChannel{{
			ID: "channel-1", ChannelType: "webhook", Config: "https://notify.example.invalid", Enabled: true,
		}}}, &mockMTSStore{}, &mockNodeStore{nodes: []model.Node{node}},
	)
	a.now = func() time.Time { return now }

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("resolve availability event: %v", err)
	}
	if event, _ := events.GetActiveEvent(t.Context(), rule.ID, node.ID); event != nil {
		t.Fatalf("active event after recovery = %#v", event)
	}
	delivered := a.notifications.(*testAlertNotifications).events[0]
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
	a := newTestAlerter(
		&mockAlertRuleStore{}, events, &mockAlertChannelStore{err: want},
		&mockMTSStore{}, &mockNodeStore{},
	)
	if err := a.resolveAlert(t.Context(), alertEvaluation{rule: rule, node: node, value: 5}); !errors.Is(err, want) {
		t.Fatalf("resolve error = %v, want %v", err, want)
	}
}

func TestEvaluateResourceRuleSkipsOfflineNode(t *testing.T) {
	rule := model.AlertRule{
		ID: "rule-1", Metric: "cpu", Operator: "gt", Threshold: 80,
	}
	mts := &mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 95}}}
	a := newTestAlerter(
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

// TestEvaluateRulesRetriggersAfterManualResolve verifies that once an alert
// event is manually resolved while the metric still exceeds the threshold, the
// next evaluation creates a fresh triggered event and pushes it to every
// enabled channel. This is the core re-trigger contract after human dismissal.
func TestEvaluateRulesRetriggersAfterManualResolve(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	node := model.Node{
		ID: "node-1", Name: "edge", Status: model.NodeStatusOnline,
		LastSeen: now.Add(-5 * time.Second),
	}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High Memory", Metric: "memory",
		Operator: "gt", Threshold: 15, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	events := &mockAlertEventStore{}

	// Seed an event that an operator manually resolved while the metric
	// remained above threshold.
	originalTrigger := now.Add(-time.Minute)
	if err := events.CreateEvent(t.Context(), &model.AlertEvent{
		ID: "event-1", RuleID: rule.ID, NodeID: node.ID,
		Message: "memory above threshold", Value: 53,
		Status: model.AlertStatusResolved, TriggeredAt: originalTrigger,
		ResolvedAt: &now,
	}); err != nil {
		t.Fatalf("seed resolved event: %v", err)
	}

	a := newTestAlerter(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		events,
		&mockAlertChannelStore{channels: []model.AlertChannel{{
			ID: "channel-1", Name: "webhook", ChannelType: "webhook",
			Config: "https://notify.example.invalid", Enabled: true,
		}}},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"memory": 53}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)
	a.now = func() time.Time { return now }

	if err := a.evaluateRules(t.Context()); err != nil {
		t.Fatalf("evaluate after manual resolve: %v", err)
	}

	// A resolved event must not block a new trigger; GetActiveEvent only
	// returns triggered/acknowledged events.
	active, err := events.GetActiveEvent(t.Context(), rule.ID, node.ID)
	if err != nil {
		t.Fatalf("get active event: %v", err)
	}
	if active == nil {
		t.Fatal("expected a new triggered event after manual resolve")
	}
	if active.Status != model.AlertStatusTriggered {
		t.Fatalf("status = %s, want triggered", active.Status)
	}
	if active.ID == "event-1" {
		t.Fatal("expected a new event, not the resolved original")
	}

	deliveries := a.notifications.(*testAlertNotifications).events
	if len(deliveries) != 1 {
		t.Fatalf("delivery count = %d, want 1", len(deliveries))
	}
	if deliveries[0].Status != model.AlertStatusTriggered {
		t.Fatalf("delivered status = %s, want triggered", deliveries[0].Status)
	}
	if !strings.Contains(deliveries[0].Message, "edge") {
		t.Fatalf("trigger message = %q, want node name", deliveries[0].Message)
	}
}
