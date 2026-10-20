package metriccatalog

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

const RetentionInterval = time.Second

const RetentionTimeout = 30 * time.Second

const retentionStateTimeout = 5 * time.Second

type RetentionStore interface {
	RecoverMetricRetention(context.Context) error
	ClaimMetricRetention(context.Context) (*model.MetricRetentionJob, error)
	CompleteMetricRetention(context.Context, model.MetricRetentionCompletion) error
}

type RetentionData interface {
	ApplyMetricRetention(context.Context, model.MetricRetentionJob) error
}

type RetentionRunner struct {
	jobs     RetentionStore
	data     RetentionData
	observer func(error)
}

func NewRetentionRunner(jobs RetentionStore, data RetentionData) *RetentionRunner {
	return &RetentionRunner{jobs: jobs, data: data}
}

func (runner *RetentionRunner) SetObserver(observer func(error)) { runner.observer = observer }

func (*RetentionRunner) Interval() time.Duration { return RetentionInterval }

func (runner *RetentionRunner) Run(ctx context.Context) {
	if err := runner.jobs.RecoverMetricRetention(ctx); err != nil {
		runner.observe(err)
	}
	ticker := time.NewTicker(RetentionInterval)
	defer ticker.Stop()
	for {
		runner.observe(runner.RunOnce(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runner *RetentionRunner) RunOnce(ctx context.Context) error {
	jobCtx, cancel := context.WithTimeout(ctx, RetentionTimeout)
	defer cancel()
	job, err := runner.jobs.ClaimMetricRetention(jobCtx)
	if err != nil || job == nil {
		return err
	}
	runErr := runner.data.ApplyMetricRetention(jobCtx, *job)
	completion := model.MetricRetentionCompletion{Job: *job, CompletedAt: model.NowUTC()}
	if runErr != nil {
		completion.Error = "metric retention failed"
	}
	stateCtx, stateCancel := context.WithTimeout(context.WithoutCancel(ctx), retentionStateTimeout)
	defer stateCancel()
	return errors.Join(runErr, runner.jobs.CompleteMetricRetention(stateCtx, completion))
}

func (runner *RetentionRunner) observe(err error) {
	if runner.observer != nil {
		runner.observer(err)
	}
}
