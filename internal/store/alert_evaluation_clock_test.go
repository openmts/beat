package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestEvaluationClockRebaseFencesPreparedWorkAndRetainsIncidents(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	state := change.Decision.State
	future := state.UpdatedAt.Add(time.Hour)
	if _, err := sqlite.DB.Exec(`UPDATE alert_evaluation_states SET last_evaluated_at=?,last_evidence_observed_at=?,next_due_at=?,trigger_elapsed_ms=1000,recovery_elapsed_ms=1000,
 health_epoch_id='11111111-1111-4111-8111-111111111111',health_status='no_data',health_elapsed_ms=1000,health_seen_mask=1,health_notified_mask=1,health_last_observed_at=?,health_status_since=?`, future.UnixMilli(), future.UnixMilli(), future.UnixMilli(), future.UnixMilli(), future.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	count, err := states.RebaseClock(t.Context(), state.UpdatedAt)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	rebased, err := states.Get(t.Context(), AlertEvaluationKey{RuleID: state.RuleID, NodeID: state.NodeID})
	if err != nil {
		t.Fatal(err)
	}
	if rebased.Version != state.Version+1 || rebased.ActiveEventID != state.ActiveEventID || rebased.Status != model.EvaluationError || rebased.Reason != "clock_rollback" || rebased.Health.EpochID == "" || rebased.Health.NotifiedMask != 1 || rebased.TriggerElapsedMS != 0 || rebased.RecoveryElapsedMS != 0 || rebased.Health.ElapsedMS != 0 {
		t.Fatal(rebased)
	}
	change.ExpectedStateVersion = state.Version
	change.Decision.State.Version = state.Version + 1
	change.Decision.Actions = nil
	if err := states.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("old plan survived rebase", err)
	}
	backlog, err := states.Backlog(t.Context(), state.UpdatedAt.Add(3*alerteval.EvaluationInterval))
	if err != nil || backlog.Due != 1 || backlog.OldestDueAge < alerteval.MaximumPollGap {
		t.Fatal(backlog, err)
	}
	count, err = states.RebaseClock(t.Context(), state.UpdatedAt)
	if err != nil || count != 0 {
		t.Fatal("rebase was not idempotent", count, err)
	}
}

func TestEvaluationClockRebaseLimitsOldLeaseWithoutStealing(t *testing.T) {
	states, pair := evaluationScheduleFixture(t)
	future := pair.DueAt.Add(time.Hour)
	lease, err := states.Claim(t.Context(), AlertEvaluationClaim{Pair: pair, WorkerID: "old", At: future})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := states.db.Exec("UPDATE alert_evaluation_states SET last_evaluated_at=?", future.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := states.RebaseClock(t.Context(), pair.DueAt); err != nil {
		t.Fatal(err)
	}
	pairs, err := states.Due(t.Context(), alerteval.DueQuery{At: pair.DueAt, Limit: 1})
	if err != nil || len(pairs) != 0 {
		t.Fatal("live lease stolen", pairs, err)
	}
	pairs, err = states.Due(t.Context(), alerteval.DueQuery{At: pair.DueAt.Add(alerteval.LeaseDuration), Limit: 1})
	if err != nil || len(pairs) != 1 {
		t.Fatal("rollback retained long future lease", pairs, err)
	}
	if err := states.Release(t.Context(), *lease); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluationClockAndBacklogRejectCancelledInvalidReads(t *testing.T) {
	states, pair := evaluationScheduleFixture(t)
	if _, err := states.RebaseClock(t.Context(), time.Time{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	if _, err := states.Backlog(t.Context(), time.Time{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	backlog, err := states.Backlog(t.Context(), pair.DueAt.Add(-time.Second))
	if err != nil || backlog.Due != 0 || backlog.OldestDueAge != 0 {
		t.Fatal(backlog, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := states.RebaseClock(ctx, pair.DueAt); err == nil {
		t.Fatal("cancelled write ignored")
	}
	if _, err := states.Backlog(ctx, pair.DueAt); err == nil {
		t.Fatal("cancelled read ignored")
	}
}
