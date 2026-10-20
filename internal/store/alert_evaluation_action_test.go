package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func healthActionOf(state model.AlertEvaluationState, event *model.AlertEvaluationEvent) evaluationAction {
	return evaluationAction{action: alerteval.Action{Kind: alerteval.ActionHealth, Health: event}, state: state}
}

func TestWriteEvaluationHealthEventRecordsEpochAndClosesOnRecovery(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	state := model.AlertEvaluationState{RuleID: "rule", NodeID: "node", RuleRevision: 2}
	occurred := model.NowUTC().Truncate(time.Millisecond)
	closed := occurred.Add(time.Minute)
	unhealthy := model.AlertEvaluationEvent{ID: "11111111-1111-1111-1111-111111111111", RuleID: "rule", NodeID: "node", RuleRevision: 2, EpochID: "33333333-3333-3333-3333-333333333333",
		Kind: model.EvaluationNoData, Reason: "missing_samples", OccurredAt: occurred, SourceKey: "alert-evaluation:33333333-3333-3333-3333-333333333333:no_data"}
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, &unhealthy)); err != nil {
		t.Fatalf("write unhealthy event: %v", err)
	}
	recovered := model.AlertEvaluationEvent{ID: "22222222-2222-2222-2222-222222222222", RuleID: "rule", NodeID: "node", RuleRevision: 2, EpochID: "33333333-3333-3333-3333-333333333333",
		Kind: "recovered", Reason: "fresh_evidence", OccurredAt: closed, ClosedAt: &closed, SourceKey: "alert-evaluation:33333333-3333-3333-3333-333333333333:recovered"}
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, &recovered)); err != nil {
		t.Fatalf("write recovery event: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	events := NewAlertEvaluationStore(sqlite.DB)
	history, err := events.ListEvents(t.Context(), AlertEvaluationQuery{RuleID: "rule", Limit: 10})
	if err != nil {
		t.Fatalf("read evaluation history: %v", err)
	}
	if len(history.Items) != 2 {
		t.Fatalf("evaluation events = %d", len(history.Items))
	}
	for _, event := range history.Items {
		if event.ClosedAt == nil {
			t.Fatalf("epoch stayed open after recovery: %#v", event)
		}
	}
	var audits int
	if err := sqlite.DB.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM admin_audit_events WHERE action IN ('alert_evaluation.no_data','alert_evaluation.recovered')").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("evaluation audits = %d", audits)
	}
}

func TestWriteEvaluationHealthEventRejectsMismatchedOrMissingEvent(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	state := model.AlertEvaluationState{RuleID: "rule", NodeID: "node", RuleRevision: 2}
	other := model.AlertEvaluationEvent{ID: "other", RuleID: "other-rule", NodeID: "node", Kind: model.EvaluationNoData, OccurredAt: model.NowUTC()}
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, &other)); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("mismatched health event = %v", err)
	}
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, nil)); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("missing health event = %v", err)
	}
}

func TestApplyThresholdRecoveryResolvesAndRejectsRepeat(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err != nil || !created {
		t.Fatalf("create active event: %v/%v", created, err)
	}
	resolvedAt := model.NowUTC()
	resolved := event
	resolved.Status, resolved.ResolvedAt, resolved.ResolutionReason = model.AlertStatusResolved, &resolvedAt, "recovered"
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery, Event: &resolved, PreviousStatus: model.AlertStatusTriggered}); err != nil {
		t.Fatalf("apply threshold recovery: %v", err)
	}
	var status model.AlertStatus
	if err := transaction.QueryRowContext(t.Context(), "SELECT status FROM alert_events WHERE id=?", event.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != model.AlertStatusResolved {
		t.Fatalf("recovered event status = %q", status)
	}
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery, Event: &resolved}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("repeated recovery = %v", err)
	}
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("missing recovery event = %v", err)
	}
}
