package alerter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/store"
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
	QueryMetrics(
		ctx context.Context,
		metrics []string,
		start time.Time,
		end time.Time,
		nodeID string,
	) (map[string][]store.TimePoint, error)
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
	observer           func(error)
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

// SetObserver 注册每轮评估的结果钩子；须在 Start 之前调用。
func (a *Alerter) SetObserver(observer func(error)) {
	a.observer = observer
}

// EvaluationInterval 返回评估周期，供监督层做滞后判定。
func (a *Alerter) EvaluationInterval() time.Duration {
	return a.evaluationInterval
}

func (a *Alerter) Start(ctx context.Context) {
	if err := a.evaluateRules(ctx); err != nil {
		slog.ErrorContext(ctx, "alert evaluation failed", "error", err)
	}
	a.notify(ctx, nil)
	ticker := time.NewTicker(a.evaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			err := a.evaluateRules(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "alert evaluation failed", "error", err)
			}
			a.notify(ctx, err)
		}
	}
}

func (a *Alerter) notify(ctx context.Context, err error) {
	if a.observer != nil {
		a.observer(err)
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

	var evalErrs error
	for _, rule := range rules {
		for _, node := range nodes {
			if err := a.evaluateRule(ctx, &rule, &node); err != nil {
				evalErrs = errors.Join(evalErrs, fmt.Errorf("evaluate alert rule %s for node %s: %w", rule.ID, node.ID, err))
			}
		}
	}

	return evalErrs
}

func (a *Alerter) evaluateRule(ctx context.Context, rule *model.AlertRule, node *model.Node) error {
	if rule.Metric != model.MetricHeartbeatAgeSeconds && node.Status == model.NodeStatusOffline {
		return nil
	}
	value, found, err := a.metricValue(ctx, rule.Metric, node)
	if err != nil {
		return err
	}
	if !found {
		return nil
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
	return nil
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
