package alerter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
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
	GetActiveEvent(ctx context.Context, ruleID, nodeID string) (*model.AlertEvent, error)
}

type Notifications interface {
	Trigger(context.Context, *model.AlertEvent) (notificationqueue.Receipt, error)
	Transition(context.Context, store.AlertEventTransition) (notificationqueue.Receipt, error)
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
	alertRuleStore  alertRuleStore
	alertEventStore alertEventStore
	notifications   Notifications
	mtsStore        mtsStore
	nodeStore       nodeStore
	trafficCycle    CycleTotalsSource

	mu                 sync.Mutex
	stopOnce           sync.Once
	states             map[string]*ruleState
	stopCh             chan struct{}
	now                func() time.Time
	offlineAfter       time.Duration
	evaluationInterval time.Duration
	observer           func(error)
}

// CycleTotalsSource 提供校准感知的当前周期流量；未注入时告警退回原始聚合。
type CycleTotalsSource interface {
	CycleTotals(ctx context.Context, node model.Node, now time.Time) (model.TrafficTotals, model.TrafficAccounting, error)
}

type Config struct {
	Rules         alertRuleStore
	Events        alertEventStore
	Metrics       mtsStore
	Nodes         nodeStore
	Notifications Notifications
	TrafficCycle  CycleTotalsSource
}

func New(config Config) *Alerter {
	return &Alerter{
		alertRuleStore:     config.Rules,
		alertEventStore:    config.Events,
		notifications:      config.Notifications,
		mtsStore:           config.Metrics,
		nodeStore:          config.Nodes,
		trafficCycle:       config.TrafficCycle,
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
	err := a.evaluateRules(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "alert evaluation failed", "error", err)
	}
	a.notify(ctx, err)
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
	if err := a.markStaleAgentNodes(ctx); err != nil {
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
	rule, err := a.freshAlertRule(ctx, rule, node)
	if err != nil {
		return err
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
		return a.triggerAlert(ctx, alertEvaluation{rule: rule, node: node, value: value})
	}
	if !conditionActive {
		return a.resolveAlert(ctx, alertEvaluation{rule: rule, node: node, value: value})
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
