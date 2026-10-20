package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

func evaluationScheduleFixture(t *testing.T) (*AlertEvaluationStore, alerteval.DuePair) {
	t.Helper()
	_, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	state := change.Decision.State
	return states, alerteval.DuePair{RuleID: state.RuleID, NodeID: state.NodeID, DueAt: time.UnixMilli(state.NextDueAt.UnixMilli()).UTC()}
}

func TestEvaluationDueLeaseOwnershipAndRetry(t *testing.T) {
	states, pair := evaluationScheduleFixture(t)
	query := alerteval.DueQuery{At: pair.DueAt, Limit: alerteval.BatchSize}
	pairs, err := states.Due(t.Context(), query)
	if err != nil || len(pairs) != 1 || pairs[0] != pair {
		t.Fatal(pairs, err)
	}
	query.After = &pair
	if pairs, err := states.Due(t.Context(), query); err != nil || len(pairs) != 0 {
		t.Fatal(pairs, err)
	}
	query.After = nil
	claim := AlertEvaluationClaim{Pair: pair, WorkerID: "worker", At: pair.DueAt}
	lease, err := states.Claim(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := states.Claim(t.Context(), claim); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	if pairs, err := states.Due(t.Context(), query); err != nil || len(pairs) != 0 {
		t.Fatal(pairs, err)
	}
	wrong := *lease
	wrong.TokenHash = make([]byte, 32)
	if err := states.Release(t.Context(), wrong); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	if err := states.Release(t.Context(), *lease); err != nil {
		t.Fatal(err)
	}
	if pairs, err := states.Due(t.Context(), query); err != nil || len(pairs) != 1 || pairs[0] != pair {
		t.Fatal(pairs, err)
	}
}

func TestEvaluationLeaseExpiryFencesOldOwner(t *testing.T) {
	states, pair := evaluationScheduleFixture(t)
	claim := AlertEvaluationClaim{Pair: pair, WorkerID: "old", At: pair.DueAt}
	old, err := states.Claim(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	claim.At = old.ExpiresAt
	claim.WorkerID = "new"
	pairs, err := states.Due(t.Context(), alerteval.DueQuery{At: claim.At, Limit: 1})
	if err != nil || len(pairs) != 1 {
		t.Fatal(pairs, err)
	}
	renewed, err := states.Claim(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if err := states.Release(t.Context(), *old); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	if err := states.Release(t.Context(), *renewed); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluationScheduleValidationAndStorageFailure(t *testing.T) {
	states, pair := evaluationScheduleFixture(t)
	for _, query := range []alerteval.DueQuery{{}, {At: pair.DueAt, Limit: alerteval.BatchSize + 1}} {
		if _, err := states.Due(t.Context(), query); !errors.Is(err, alerteval.ErrInvalidPolicy) {
			t.Fatal(err)
		}
	}
	if _, err := states.Claim(t.Context(), AlertEvaluationClaim{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	if err := states.Release(t.Context(), AlertEvaluationLease{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := states.Due(ctx, alerteval.DueQuery{At: pair.DueAt, Limit: 1}); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := states.Claim(ctx, AlertEvaluationClaim{Pair: pair, At: pair.DueAt, WorkerID: "worker"}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestEvaluationAdmissionCreatesMissingPairsWithoutResettingProgress(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	at := time.Now().UTC()
	if _, err := states.Admit(t.Context(), time.Time{}); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	count, err := states.Admit(t.Context(), at)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	state, err := states.Get(t.Context(), AlertEvaluationKey{RuleID: change.Decision.State.RuleID, NodeID: change.Decision.State.NodeID})
	if err != nil || state == nil || state.Version != 1 || state.Status != "no_data" {
		t.Fatal(state, err)
	}
	count, err = states.Admit(t.Context(), at.Add(time.Minute))
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err := sqlite.DB.Exec("UPDATE alert_rules SET enabled=0"); err != nil {
		t.Fatal(err)
	}
	count, err = states.Admit(t.Context(), at)
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := states.Admit(ctx, at); err == nil {
		t.Fatal("cancelled admission succeeded")
	}
}
