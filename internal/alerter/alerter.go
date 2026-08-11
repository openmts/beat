package alerter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

const (
	defaultEvaluationInterval = 30 * time.Second
	defaultOfflineAfter       = 90 * time.Second
)

type alertRuleStore interface {
	ListEnabledRules(ctx context.Context) ([]model.AlertRule, error)
}

type alertEventStore interface {
	CreateEvent(ctx context.Context, event *model.AlertEvent) error
	GetActiveEvent(ctx context.Context, ruleID, nodeID string) (*model.AlertEvent, error)
	UpdateEvent(ctx context.Context, event *model.AlertEvent) error
}

type alertChannelStore interface {
	ListEnabledChannels(ctx context.Context) ([]model.AlertChannel, error)
}

type deliveryService interface {
	Send(
		ctx context.Context,
		channel *model.AlertChannel,
		event *model.AlertEvent,
	) (model.AlertDeliveryStatus, error)
}

type mtsStore interface {
	QueryLatest(ctx context.Context, nodeID string) (map[string]float64, error)
	QueryTrafficUsage(
		ctx context.Context,
		nodeID string,
		start time.Time,
		end time.Time,
	) (model.TrafficTotals, error)
}

type nodeStore interface {
	ListNodes(ctx context.Context, groupID string) ([]model.Node, error)
	MarkStaleNodesOffline(ctx context.Context, cutoff time.Time) (int64, error)
}

type ruleState struct {
	triggeredAt time.Time
	active      bool
}

type Alerter struct {
	alertRuleStore    alertRuleStore
	alertEventStore   alertEventStore
	alertChannelStore alertChannelStore
	delivery          deliveryService
	mtsStore          mtsStore
	nodeStore         nodeStore

	mu                 sync.Mutex
	stopOnce           sync.Once
	states             map[string]*ruleState
	stopCh             chan struct{}
	now                func() time.Time
	offlineAfter       time.Duration
	evaluationInterval time.Duration
}

func New(
	alertRuleStore alertRuleStore,
	alertEventStore alertEventStore,
	alertChannelStore alertChannelStore,
	mtsStore mtsStore,
	nodeStore nodeStore,
	deliveryServices ...deliveryService,
) *Alerter {
	delivery := deliveryService(notification.NewService())
	if len(deliveryServices) > 0 && deliveryServices[0] != nil {
		delivery = deliveryServices[0]
	}
	return &Alerter{
		alertRuleStore:     alertRuleStore,
		alertEventStore:    alertEventStore,
		alertChannelStore:  alertChannelStore,
		delivery:           delivery,
		mtsStore:           mtsStore,
		nodeStore:          nodeStore,
		states:             make(map[string]*ruleState),
		stopCh:             make(chan struct{}),
		now:                model.NowUTC,
		offlineAfter:       defaultOfflineAfter,
		evaluationInterval: defaultEvaluationInterval,
	}
}

func (a *Alerter) Start(ctx context.Context) {
	if err := a.evaluateRules(ctx); err != nil {
		slog.ErrorContext(ctx, "alert evaluation failed", "error", err)
	}
	ticker := time.NewTicker(a.evaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			if err := a.evaluateRules(ctx); err != nil {
				slog.ErrorContext(ctx, "alert evaluation failed", "error", err)
			}
		}
	}
}

func (a *Alerter) Stop() {
	a.stopOnce.Do(func() { close(a.stopCh) })
}

func (a *Alerter) evaluateRules(ctx context.Context) error {
	if _, err := a.nodeStore.MarkStaleNodesOffline(ctx, a.now().Add(-a.offlineAfter)); err != nil {
		return fmt.Errorf("mark stale nodes offline: %w", err)
	}
	rules, err := a.alertRuleStore.ListEnabledRules(ctx)
	if err != nil {
		return fmt.Errorf("list enabled rules: %w", err)
	}

	nodes, err := a.nodeStore.ListNodes(ctx, "")
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	for _, rule := range rules {
		for _, node := range nodes {
			a.evaluateRule(ctx, &rule, &node)
		}
	}

	return nil
}

func (a *Alerter) evaluateRule(ctx context.Context, rule *model.AlertRule, node *model.Node) {
	if rule.Metric != model.MetricHeartbeatAgeSeconds && node.Status == model.NodeStatusOffline {
		return
	}
	value, found, err := a.metricValue(ctx, rule.Metric, node)
	if err != nil || !found {
		return
	}

	trigger, conditionActive := a.updateRuleState(rule, node, value)
	if trigger {
		if err := a.triggerAlert(ctx, rule, node, value); err != nil {
			slog.ErrorContext(ctx, "alert trigger failed", "rule_id", rule.ID, "node_id", node.ID, "error", err)
		}
	}
	if !conditionActive {
		if err := a.resolveAlert(ctx, rule, node, value); err != nil {
			slog.ErrorContext(ctx, "alert resolution failed", "rule_id", rule.ID, "node_id", node.ID, "error", err)
		}
	}
}

func (a *Alerter) updateRuleState(
	rule *model.AlertRule,
	node *model.Node,
	value float64,
) (bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stateKey := rule.ID + ":" + node.ID
	conditionActive := evaluateThreshold(value, rule.Operator, rule.Threshold)
	if !conditionActive {
		delete(a.states, stateKey)
		return false, false
	}
	state := a.states[stateKey]
	if state == nil {
		state = &ruleState{active: true, triggeredAt: a.now()}
		a.states[stateKey] = state
		return rule.Duration <= 0, true
	}
	ready := a.now().Sub(state.triggeredAt) >= time.Duration(rule.Duration)*time.Second
	return ready, true
}
