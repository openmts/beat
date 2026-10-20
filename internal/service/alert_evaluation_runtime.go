package service

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type AlertEvaluatorConfig struct {
	Database *sql.DB
	Metrics  *store.MTSStore
	Queue    notificationqueue.BatchEnqueuer
	Workers  int
}

type AlertEvaluator struct {
	states        *store.AlertEvaluationStore
	rules         *store.AlertEvaluationRuleStore
	nodes         *store.NodeStore
	events        *store.AlertEventStore
	catalog       *store.MetricCatalogStore
	custom        *store.CustomMetricStore
	metrics       *store.MTSStore
	notifications *AlertEvaluationNotificationService
	scheduler     *alerteval.Scheduler
	runID         string
	now           func() time.Time
	running       atomic.Bool
	previews      evaluationPreviewAdmission
}

func NewAlertEvaluator(config AlertEvaluatorConfig) (*AlertEvaluator, error) {
	if config.Database == nil || config.Metrics == nil || config.Queue == nil {
		return nil, alerteval.ErrInvalidPolicy
	}
	runID, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	evaluator := &AlertEvaluator{states: store.NewAlertEvaluationStore(config.Database), rules: store.NewAlertEvaluationRuleStore(config.Database),
		nodes: store.NewNodeStore(config.Database), events: store.NewAlertEventStore(config.Database), catalog: store.NewMetricCatalogStore(config.Database),
		custom: store.NewCustomMetricStore(config.Database), metrics: config.Metrics, runID: runID.String(), now: model.NowUTC}
	evaluator.notifications = NewAlertEvaluationNotificationService(evaluator.states, config.Queue)
	evaluator.scheduler, err = alerteval.NewScheduler(evaluator, config.Workers)
	if err != nil {
		return nil, err
	}
	return evaluator, nil
}

func (e *AlertEvaluator) RunOnce(ctx context.Context) (alerteval.RoundResult, error) {
	if !e.running.CompareAndSwap(false, true) {
		return alerteval.RoundResult{}, alerteval.ErrConflict
	}
	defer e.running.Store(false)
	ctx, cancel := context.WithTimeout(ctx, alerteval.RoundBudget)
	defer cancel()
	at := e.now()
	if _, err := e.states.RebaseClock(ctx, at); err != nil {
		return alerteval.RoundResult{}, err
	}
	for {
		count, err := e.states.Admit(ctx, at)
		if err != nil {
			return alerteval.RoundResult{}, err
		}
		if count < alerteval.BatchSize {
			break
		}
	}
	return e.evaluateRound(ctx, at)
}

func (e *AlertEvaluator) Due(ctx context.Context, query alerteval.DueQuery) ([]alerteval.DuePair, error) {
	return e.states.Due(ctx, query)
}

func (e *AlertEvaluator) Run(ctx context.Context, observe func(alerteval.RoundResult, error)) {
	ticker := time.NewTicker(alerteval.EvaluationInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		result, err := e.RunOnce(ctx)
		if observe != nil {
			observe(result, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *AlertEvaluator) Evaluate(ctx context.Context, pair alerteval.DuePair) (result error) {
	lease, err := e.states.Claim(ctx, store.AlertEvaluationClaim{Pair: pair, WorkerID: e.runID, At: e.now()})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), alerteval.CommitTimeout)
		defer cancel()
		err := e.states.Release(cleanup, *lease)
		if err != nil && !errors.Is(err, alerteval.ErrConflict) {
			result = errors.Join(result, err)
		}
	}()
	input, policies, err := e.prepare(ctx, lease.Key)
	if err != nil {
		return err
	}
	decision, err := alerteval.Step(input)
	if err != nil {
		return err
	}
	_, err = e.notifications.Commit(ctx, store.AlertEvaluationCommit{Decision: decision, ExpectedStateVersion: input.Previous.Version,
		RuleRevision: input.Rule.Revision, Policies: policies, LeaseTokenHash: lease.TokenHash})
	return err
}
