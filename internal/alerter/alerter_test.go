package alerter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	beatstore "github.com/beat/backend/internal/store"
)

type mockAlertRuleStore struct {
	rules []model.AlertRule
	err   error
}

func (m *mockAlertRuleStore) ListEnabledRules(_ context.Context) ([]model.AlertRule, error) {
	return m.rules, m.err
}

type mockAlertEventStore struct {
	mu           sync.Mutex
	events       map[string]*model.AlertEvent
	createErr    error
	getActiveErr error
	updateErr    error
}

func (m *mockAlertEventStore) key(ruleID, nodeID string) string {
	return ruleID + ":" + nodeID
}

func (m *mockAlertEventStore) CreateEvent(_ context.Context, event *model.AlertEvent) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = make(map[string]*model.AlertEvent)
	}
	m.events[m.key(event.RuleID, event.NodeID)] = event
	return nil
}

func (m *mockAlertEventStore) GetActiveEvent(_ context.Context, ruleID, nodeID string) (*model.AlertEvent, error) {
	if m.getActiveErr != nil {
		return nil, m.getActiveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		return nil, nil
	}
	ev, ok := m.events[m.key(ruleID, nodeID)]
	if !ok {
		return nil, nil
	}
	if ev.Status == model.AlertStatusResolved {
		return nil, nil
	}
	return ev, nil
}

func (m *mockAlertEventStore) UpdateEvent(_ context.Context, event *model.AlertEvent) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = make(map[string]*model.AlertEvent)
	}
	m.events[m.key(event.RuleID, event.NodeID)] = event
	return nil
}

type mockAlertChannelStore struct {
	channels []model.AlertChannel
	err      error
}

func (m *mockAlertChannelStore) ListEnabledChannels(_ context.Context) ([]model.AlertChannel, error) {
	return m.channels, m.err
}

type mockMTSStore struct {
	metrics        map[string]map[string]float64
	traffic        map[string]model.TrafficTotals
	err            error
	trafficErr     error
	trafficQueries int
	latestQueries  int
}

func (m *mockMTSStore) QueryLatest(_ context.Context, nodeID string) (map[string]float64, error) {
	m.latestQueries++
	if m.err != nil {
		return nil, m.err
	}
	if m.metrics == nil {
		return nil, nil
	}
	return m.metrics[nodeID], nil
}

func (m *mockMTSStore) QueryTrafficUsage(
	_ context.Context,
	nodeID string,
	_ time.Time,
	_ time.Time,
) (model.TrafficTotals, error) {
	m.trafficQueries++
	if m.trafficErr != nil {
		return model.TrafficTotals{}, m.trafficErr
	}
	return m.traffic[nodeID], nil
}

type mockNodeStore struct {
	nodes     []model.Node
	err       error
	markErr   error
	markCalls int
	cutoff    time.Time
}

func (m *mockNodeStore) ListOnlineNodes(_ context.Context) ([]model.Node, error) {
	return m.nodes, m.err
}

func (m *mockNodeStore) ListNodes(_ context.Context, _ string) ([]model.Node, error) {
	return m.nodes, m.err
}

func (m *mockNodeStore) MarkStaleNodesOffline(_ context.Context, cutoff time.Time) (int64, error) {
	m.markCalls++
	m.cutoff = cutoff
	return 0, m.markErr
}

func TestNew(t *testing.T) {
	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	if a.alertRuleStore == nil {
		t.Error("expected alertRuleStore to be set")
	}
	if a.alertEventStore == nil {
		t.Error("expected alertEventStore to be set")
	}
	if a.alertChannelStore == nil {
		t.Error("expected alertChannelStore to be set")
	}
	if a.mtsStore == nil {
		t.Error("expected mtsStore to be set")
	}
	if a.nodeStore == nil {
		t.Error("expected nodeStore to be set")
	}
	if a.states == nil {
		t.Error("expected states map to be initialized")
	}
	if a.delivery == nil {
		t.Error("expected delivery service to be initialized")
	}
	if a.stopCh == nil {
		t.Error("expected stopCh to be initialized")
	}
}

func TestStop(t *testing.T) {
	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		a.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	a.Stop()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("alerter did not stop within timeout")
	}
}

func TestStopIdempotent(t *testing.T) {
	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	a.Stop()
	a.Stop()
}

func TestEvaluateRulesNoRules(t *testing.T) {
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{}},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestEvaluateRulesNoNodes(t *testing.T) {
	a := New(
		&mockAlertRuleStore{
			rules: []model.AlertRule{
				{ID: "rule-1", Metric: "cpu", Operator: "gt", Threshold: 80, Duration: 0},
			},
		},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{nodes: []model.Node{}},
	)

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestEvaluateRulesListRulesError(t *testing.T) {
	a := New(
		&mockAlertRuleStore{err: fmt.Errorf("db error")},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.evaluateRules(context.Background())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEvaluateRulesListNodesError(t *testing.T) {
	a := New(
		&mockAlertRuleStore{
			rules: []model.AlertRule{
				{ID: "rule-1", Metric: "cpu", Operator: "gt", Threshold: 80, Duration: 0},
			},
		},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{err: fmt.Errorf("db error")},
	)

	err := a.evaluateRules(context.Background())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEvaluateRulesThresholdCrossed(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}
	channelStore := &mockAlertChannelStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		channelStore,
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 90}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	_ = a.evaluateRules(context.Background())
	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev == nil {
		t.Fatal("expected alert event to be created")
	}
	if ev.Status != model.AlertStatusTriggered {
		t.Errorf("expected status triggered, got %s", ev.Status)
	}
}

func TestEvaluateRulesThresholdNotCrossed(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 50}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev != nil {
		t.Error("expected no alert event when threshold not crossed")
	}
}

func TestEvaluateRulesDurationElapsed(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 5,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 90}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	stateKey := rule.ID + ":" + node.ID
	a.mu.Lock()
	a.states[stateKey] = &ruleState{
		active:      true,
		triggeredAt: time.Now().Add(-10 * time.Second),
	}
	a.mu.Unlock()

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev == nil {
		t.Fatal("expected alert event to be created after duration elapsed")
	}
}

func TestEvaluateRulesDurationNotMet(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 300,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 90}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	stateKey := rule.ID + ":" + node.ID
	a.mu.Lock()
	a.states[stateKey] = &ruleState{
		active:      true,
		triggeredAt: time.Now(),
	}
	a.mu.Unlock()

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev != nil {
		t.Error("expected no alert event when duration not met")
	}
}

func TestEvaluateRulesResolve(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 50}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	_ = eventStore.CreateEvent(context.Background(), &model.AlertEvent{
		ID: "event-1", RuleID: rule.ID, NodeID: node.ID,
		Status: model.AlertStatusTriggered, TriggeredAt: time.Now(),
	})

	stateKey := rule.ID + ":" + node.ID
	a.mu.Lock()
	a.states[stateKey] = &ruleState{active: true}
	a.mu.Unlock()

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev != nil {
		t.Error("expected alert event to be resolved")
	}
}

func TestEvaluateRulesExistingActiveEvent(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "High CPU", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	_ = eventStore.CreateEvent(context.Background(), &model.AlertEvent{
		ID: "event-1", RuleID: rule.ID, NodeID: node.ID,
		Status: model.AlertStatusTriggered, TriggeredAt: time.Now(),
	})

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 90}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	stateKey := rule.ID + ":" + node.ID
	a.mu.Lock()
	a.states[stateKey] = &ruleState{
		active:      true,
		triggeredAt: time.Now().Add(-10 * time.Second),
	}
	a.mu.Unlock()

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	eventStore.mu.Lock()
	eventCount := len(eventStore.events)
	eventStore.mu.Unlock()
	if eventCount != 1 {
		t.Errorf("expected 1 event, got %d", eventCount)
	}
}

func TestEvaluateRulesMetricNotFound(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "Disk Alert", Metric: "disk_io",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"cpu": 90}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev != nil {
		t.Error("expected no alert event when metric not found")
	}
}

func TestEvaluateRulesMTSQueryError(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "CPU Alert", Metric: "cpu",
		Operator: "gt", Threshold: 80, Duration: 0,
		Severity: model.AlertSeverityWarning,
	}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{err: fmt.Errorf("mts error")},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error (errors are skipped), got %v", err)
	}
}

func TestEvaluateRulesTrafficQuotaUsage(t *testing.T) {
	trackedSince := time.Now().Add(-time.Hour)
	node := model.Node{
		ID: "node-1", Name: "node-1", TrafficLimit: 1_000,
		TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1,
	}
	rule := model.AlertRule{
		ID: "rule-traffic", Name: "Traffic quota", Metric: model.MetricTrafficUsagePercent,
		Operator: "gt", Threshold: 80, Duration: 0, Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}
	mtsStore := &mockMTSStore{traffic: map[string]model.TrafficTotals{
		node.ID: {Sent: 400, Received: 500, TrackedSince: &trackedSince},
	}}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, eventStore,
		&mockAlertChannelStore{}, mtsStore, &mockNodeStore{nodes: []model.Node{node}},
	)

	for range 3 {
		if err := a.evaluateRules(context.Background()); err != nil {
			t.Fatalf("evaluate traffic quota rule: %v", err)
		}
	}

	event, err := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if err != nil {
		t.Fatalf("get traffic quota event: %v", err)
	}
	if event == nil || event.Value != 90 {
		t.Fatalf("traffic quota event = %#v, want value 90", event)
	}
	eventStore.mu.Lock()
	eventCount := len(eventStore.events)
	eventStore.mu.Unlock()
	if eventCount != 1 {
		t.Fatalf("traffic quota event count = %d, want 1", eventCount)
	}
}

func TestEvaluateRulesTrafficQuotaUsesMTSAggregation(t *testing.T) {
	ctx := t.Context()
	mtsStore, err := beatstore.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	now := model.NowUTC()
	samples := []beatstore.NodeMetricSample{
		{
			NodeID: "node-1", Timestamp: now.Add(-2 * time.Minute),
			Metrics: model.NodeMetrics{NetRecvTotal: 100, NetSentTotal: 200},
		},
		{
			NodeID: "node-1", Timestamp: now.Add(-time.Minute),
			Metrics: model.NodeMetrics{NetRecvTotal: 500, NetSentTotal: 600},
		},
	}
	for _, sample := range samples {
		if err := mtsStore.WriteNodeMetrics(ctx, sample); err != nil {
			t.Fatalf("write MTS sample: %v", err)
		}
	}
	if err := mtsStore.Flush(ctx); err != nil {
		t.Fatalf("flush MTS samples: %v", err)
	}

	node := model.Node{
		ID: "node-1", Name: "node-1", TrafficLimit: 1_000,
		TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1,
	}
	rule := model.AlertRule{
		ID: "rule-traffic", Name: "Traffic quota", Metric: model.MetricTrafficUsagePercent,
		Operator: "gt", Threshold: 70, Duration: 0, Severity: model.AlertSeverityWarning,
	}
	eventStore := &mockAlertEventStore{}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, eventStore,
		&mockAlertChannelStore{}, mtsStore, &mockNodeStore{nodes: []model.Node{node}},
	)
	for range 2 {
		if err := a.evaluateRules(ctx); err != nil {
			t.Fatalf("evaluate MTS traffic quota: %v", err)
		}
	}

	event, err := eventStore.GetActiveEvent(ctx, rule.ID, node.ID)
	if err != nil {
		t.Fatalf("get MTS traffic quota event: %v", err)
	}
	if event == nil || event.Value != 80 {
		t.Fatalf("MTS traffic quota event = %#v, want value 80", event)
	}
}

func TestEvaluateRulesTrafficQuotaSkipsUnavailableUsage(t *testing.T) {
	tests := []struct {
		name        string
		node        model.Node
		totals      model.TrafficTotals
		wantQueries int
	}{
		{
			name: "unlimited node",
			node: model.Node{ID: "node-1", TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1},
		},
		{
			name: "tracking not started",
			node: model.Node{
				ID: "node-1", TrafficLimit: 1_000,
				TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1,
			},
			wantQueries: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := model.AlertRule{
				ID: "rule", Metric: model.MetricTrafficUsagePercent,
				Operator: "gt", Threshold: 0, Duration: 0,
			}
			eventStore := &mockAlertEventStore{}
			mtsStore := &mockMTSStore{traffic: map[string]model.TrafficTotals{test.node.ID: test.totals}}
			a := New(
				&mockAlertRuleStore{rules: []model.AlertRule{rule}}, eventStore,
				&mockAlertChannelStore{}, mtsStore, &mockNodeStore{nodes: []model.Node{test.node}},
			)

			if err := a.evaluateRules(context.Background()); err != nil {
				t.Fatalf("evaluate traffic quota rule: %v", err)
			}
			if mtsStore.trafficQueries != test.wantQueries {
				t.Fatalf("traffic queries = %d, want %d", mtsStore.trafficQueries, test.wantQueries)
			}
			if event, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, test.node.ID); event != nil {
				t.Fatalf("unexpected traffic quota event: %#v", event)
			}
		})
	}
}

func TestEvaluateRulesTrafficQuotaQueryErrorIsSkipped(t *testing.T) {
	node := model.Node{
		ID: "node-1", TrafficLimit: 1_000,
		TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1,
	}
	rule := model.AlertRule{
		ID: "rule", Metric: model.MetricTrafficUsagePercent,
		Operator: "gt", Threshold: 80, Duration: 0,
	}
	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}}, &mockAlertEventStore{},
		&mockAlertChannelStore{}, &mockMTSStore{trafficErr: fmt.Errorf("mts error")},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	if err := a.evaluateRules(context.Background()); err != nil {
		t.Fatalf("traffic storage errors should be skipped, got %v", err)
	}
}

func TestEvaluateRulesLTThreshold(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "node-1"}
	rule := model.AlertRule{
		ID: "rule-1", Name: "Low Memory", Metric: "memory",
		Operator: "lt", Threshold: 20, Duration: 0,
		Severity: model.AlertSeverityCritical,
	}
	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{rules: []model.AlertRule{rule}},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{metrics: map[string]map[string]float64{"node-1": {"memory": 10}}},
		&mockNodeStore{nodes: []model.Node{node}},
	)

	_ = a.evaluateRules(context.Background())
	err := a.evaluateRules(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ev, _ := eventStore.GetActiveEvent(context.Background(), rule.ID, node.ID)
	if ev == nil {
		t.Fatal("expected alert event for lt operator")
	}
}

func TestEvaluateThreshold(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		{name: "gt_true", value: 90, operator: "gt", threshold: 80, expected: true},
		{name: "gt_false", value: 70, operator: "gt", threshold: 80, expected: false},
		{name: "gt_equal", value: 80, operator: "gt", threshold: 80, expected: false},
		{name: "lt_true", value: 10, operator: "lt", threshold: 20, expected: true},
		{name: "lt_false", value: 30, operator: "lt", threshold: 20, expected: false},
		{name: "lt_equal", value: 20, operator: "lt", threshold: 20, expected: false},
		{name: "unknown_operator", value: 90, operator: "gte", threshold: 80, expected: false},
		{name: "empty_operator", value: 90, operator: "", threshold: 80, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateThreshold(tt.value, tt.operator, tt.threshold)
			if got != tt.expected {
				t.Errorf("evaluateThreshold(%f, %q, %f) = %v, want %v",
					tt.value, tt.operator, tt.threshold, got, tt.expected)
			}
		})
	}
}

func TestFormatAlertMessage(t *testing.T) {
	rule := &model.AlertRule{
		Name:      "High CPU",
		Metric:    "cpu",
		Operator:  "gt",
		Threshold: 80,
		Severity:  model.AlertSeverityWarning,
	}
	node := &model.Node{Name: "web-server-01"}

	msg := formatAlertMessage(rule, node, 95.5)

	if !strings.Contains(msg, "High CPU") {
		t.Error("expected message to contain rule name")
	}
	if !strings.Contains(msg, "web-server-01") {
		t.Error("expected message to contain node name")
	}
	if !strings.Contains(msg, "95.50") {
		t.Error("expected message to contain value")
	}
	if !strings.Contains(msg, "80.00") {
		t.Error("expected message to contain threshold")
	}
	if !strings.Contains(msg, "gt") {
		t.Error("expected message to contain operator")
	}
	if !strings.Contains(msg, "warning") {
		t.Error("expected message to contain severity")
	}
}

func TestPushToChannelsWebhook(t *testing.T) {
	serverReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverReceived = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channelStore := &mockAlertChannelStore{
		channels: []model.AlertChannel{
			{ID: "ch-1", Name: "Webhook", ChannelType: "webhook", Config: server.URL, Enabled: true},
		},
	}

	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		channelStore,
		&mockMTSStore{},
		&mockNodeStore{},
	)

	event := &model.AlertEvent{
		ID: "ev-1", RuleID: "rule-1", NodeID: "node-1",
		Message: "test", Value: 95, Status: model.AlertStatusTriggered,
		TriggeredAt: time.Now(),
	}

	err := a.pushToChannels(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !serverReceived {
		t.Error("expected webhook server to receive request")
	}
}

func TestPushToChannelsListError(t *testing.T) {
	channelStore := &mockAlertChannelStore{err: fmt.Errorf("db error")}

	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		channelStore,
		&mockMTSStore{},
		&mockNodeStore{},
	)

	event := &model.AlertEvent{
		ID: "ev-1", RuleID: "rule-1", NodeID: "node-1",
		Message: "test", Value: 95, Status: model.AlertStatusTriggered,
		TriggeredAt: time.Now(),
	}

	err := a.pushToChannels(context.Background(), event)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPushToChannelsUnknownType(t *testing.T) {
	channelStore := &mockAlertChannelStore{
		channels: []model.AlertChannel{
			{ID: "ch-1", Name: "Unknown", ChannelType: "sms", Config: "{}", Enabled: true},
		},
	}

	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		channelStore,
		&mockMTSStore{},
		&mockNodeStore{},
	)

	event := &model.AlertEvent{
		ID: "ev-1", RuleID: "rule-1", NodeID: "node-1",
		Message: "test", Value: 95, Status: model.AlertStatusTriggered,
		TriggeredAt: time.Now(),
	}

	err := a.pushToChannels(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error for unknown channel type, got %v", err)
	}
}

func TestTriggerAlertGetActiveEventError(t *testing.T) {
	rule := &model.AlertRule{ID: "rule-1", Name: "test", Metric: "cpu", Severity: model.AlertSeverityWarning}
	node := &model.Node{ID: "node-1", Name: "test"}

	eventStore := &mockAlertEventStore{getActiveErr: fmt.Errorf("db error")}

	a := New(
		&mockAlertRuleStore{},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.triggerAlert(context.Background(), rule, node, 90)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestTriggerAlertCreateEventError(t *testing.T) {
	rule := &model.AlertRule{ID: "rule-1", Name: "test", Metric: "cpu", Severity: model.AlertSeverityWarning}
	node := &model.Node{ID: "node-1", Name: "test"}

	eventStore := &mockAlertEventStore{createErr: fmt.Errorf("db error")}

	a := New(
		&mockAlertRuleStore{},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.triggerAlert(context.Background(), rule, node, 90)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestResolveAlertGetActiveEventError(t *testing.T) {
	rule := &model.AlertRule{ID: "rule-1", Name: "test", Metric: "cpu", Severity: model.AlertSeverityWarning}
	node := &model.Node{ID: "node-1", Name: "test"}

	eventStore := &mockAlertEventStore{getActiveErr: fmt.Errorf("db error")}

	a := New(
		&mockAlertRuleStore{},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.resolveAlert(context.Background(), rule, node, 50)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestResolveAlertNoActiveEvent(t *testing.T) {
	rule := &model.AlertRule{ID: "rule-1", Name: "test", Metric: "cpu", Severity: model.AlertSeverityWarning}
	node := &model.Node{ID: "node-1", Name: "test"}

	eventStore := &mockAlertEventStore{}

	a := New(
		&mockAlertRuleStore{},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.resolveAlert(context.Background(), rule, node, 50)
	if err != nil {
		t.Errorf("expected no error when no active event, got %v", err)
	}
}

func TestResolveAlertUpdateEventError(t *testing.T) {
	rule := &model.AlertRule{ID: "rule-1", Name: "test", Metric: "cpu", Severity: model.AlertSeverityWarning}
	node := &model.Node{ID: "node-1", Name: "test"}

	eventStore := &mockAlertEventStore{updateErr: fmt.Errorf("db error")}
	_ = eventStore.CreateEvent(context.Background(), &model.AlertEvent{
		ID: "ev-1", RuleID: rule.ID, NodeID: node.ID,
		Status: model.AlertStatusTriggered, TriggeredAt: time.Now(),
	})

	a := New(
		&mockAlertRuleStore{},
		eventStore,
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	err := a.resolveAlert(context.Background(), rule, node, 50)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestStartContextCancellation(t *testing.T) {
	a := New(
		&mockAlertRuleStore{},
		&mockAlertEventStore{},
		&mockAlertChannelStore{},
		&mockMTSStore{},
		&mockNodeStore{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("alerter did not stop on context cancellation")
	}
}
