package service

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

func (e *AlertEvaluator) evaluateRound(ctx context.Context, at time.Time) (alerteval.RoundResult, error) {
	before, err := e.states.Backlog(ctx, at)
	if err != nil {
		return alerteval.RoundResult{}, err
	}
	result, roundErr := e.scheduler.Round(ctx, at)
	result.Deferred = max(before.Due-result.Completed, 0)
	result.OldestDueAge = before.OldestDueAge
	result.Degraded = result.OldestDueAge > alerteval.MaximumPollGap
	if ctx.Err() != nil {
		return result, roundErr
	}
	after, err := e.states.Backlog(ctx, e.now())
	if err != nil {
		return result, errors.Join(roundErr, err)
	}
	result.Deferred = after.Due
	result.OldestDueAge = after.OldestDueAge
	result.BacklogComplete = true
	result.Degraded = after.OldestDueAge > alerteval.MaximumPollGap
	return result, roundErr
}
