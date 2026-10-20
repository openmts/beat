package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

const (
	covEvaluationEpoch  = "44444444-4444-4444-4444-444444444444"
	covHealthEventID    = "11111111-1111-4111-8111-111111111111"
	covBlockedEventID   = "11111111-1111-4111-8111-222222222222"
	covRecoveredEventID = "11111111-1111-4111-8111-333333333333"
	covRecoveredKind    = "recovered"
	covScalarNodeName   = "scope-node"
)

func TestApplyEvaluationActionDispatchesHealthEvents(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	state := model.AlertEvaluationState{RuleID: "rule", NodeID: covScalarNodeName, RuleRevision: 1}
	event := model.AlertEvaluationEvent{ID: covHealthEventID, RuleID: state.RuleID, NodeID: state.NodeID, RuleRevision: 1,
		EpochID: covEvaluationEpoch, Kind: model.EvaluationNoData, Reason: "missing_samples", OccurredAt: model.NowUTC(), SourceKey: "cov:health"}
	if err := applyEvaluationAction(t.Context(), transaction, healthActionOf(state, &event)); err != nil {
		t.Fatalf("health dispatch: %v", err)
	}
	var stored int
	if err := transaction.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM alert_evaluation_events WHERE id=?", event.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("health events = %d", stored)
	}
}

func TestApplyEvaluationActionRejectsUnknownAndUnguardedActions(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	state := model.AlertEvaluationState{RuleID: event.RuleID, NodeID: event.NodeID, RuleRevision: 1}
	foreign := event
	foreign.RuleID = "other-rule"
	if err := applyEvaluationAction(t.Context(), transaction, evaluationAction{action: alerteval.Action{Kind: alerteval.ActionThresholdTrigger, Event: &foreign}, state: state}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("trigger for a foreign rule = %v", err)
	}
	if err := applyEvaluationAction(t.Context(), transaction, evaluationAction{action: alerteval.Action{Kind: alerteval.ActionThresholdTrigger}, state: state}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("trigger without an incident = %v", err)
	}
	if err := applyEvaluationAction(t.Context(), transaction, evaluationAction{action: alerteval.Action{Kind: "unknown_action"}, state: state}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("unknown evaluation action = %v", err)
	}
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err != nil || !created {
		t.Fatalf("seed incident: %v/%v", created, err)
	}
	if err := applyEvaluationAction(t.Context(), transaction, evaluationAction{action: alerteval.Action{Kind: alerteval.ActionThresholdTrigger, Event: &event}, state: state}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("duplicate trigger = %v", err)
	}
}

func TestApplyEvaluationActionTransitionsUnavailableDevices(t *testing.T) {
	sqlite := setupTestDB(t)
	now := model.NowUTC()
	incident := model.AlertEvent{ID: "cov-device-incident", RuleID: "rule", NodeID: covScalarNodeName, DeviceID: covAcceleratorDevice,
		Value: 1, Status: model.AlertStatusTriggered, TriggeredAt: now}
	if err := NewAlertEventStore(sqlite.DB).CreateEvent(t.Context(), &incident); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	state := model.AlertEvaluationState{RuleID: incident.RuleID, NodeID: incident.NodeID, DeviceID: incident.DeviceID, RuleRevision: 1}
	resolved := incident
	resolved.Status, resolved.ResolvedAt, resolved.ResolutionReason = model.AlertStatusResolved, &now, model.AcceleratorDeviceUnavailable
	action := evaluationAction{action: alerteval.Action{Kind: alerteval.ActionDeviceUnavailable, Event: &resolved, PreviousStatus: model.AlertStatusTriggered}, state: state}
	if err := applyEvaluationAction(t.Context(), transaction, action); err != nil {
		t.Fatalf("device unavailable transition: %v", err)
	}
	var status model.AlertStatus
	if err := transaction.QueryRowContext(t.Context(), "SELECT status FROM alert_events WHERE id=?", incident.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != model.AlertStatusResolved {
		t.Fatalf("device incident status = %q", status)
	}
}

func TestApplyEvaluationActionRejectsUnavailableDeviceConflicts(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	now := model.NowUTC()
	state := model.AlertEvaluationState{RuleID: "rule", NodeID: covScalarNodeName, DeviceID: covAcceleratorDevice, RuleRevision: 1}
	cases := []struct {
		name  string
		event *model.AlertEvent
	}{
		{"missing event", nil},
		{"missing device identity", &model.AlertEvent{ID: "cov-no-device", RuleID: "rule", NodeID: covScalarNodeName, Status: model.AlertStatusResolved, ResolvedAt: &now}},
		{"scalar resolution reason", &model.AlertEvent{ID: "cov-scalar-reason", RuleID: "rule", NodeID: covScalarNodeName, DeviceID: covAcceleratorDevice,
			Status: model.AlertStatusResolved, ResolvedAt: &now, ResolutionReason: "recovered"}},
		{"unpersisted incident", &model.AlertEvent{ID: "cov-absent-incident", RuleID: "rule", NodeID: covScalarNodeName, DeviceID: covAcceleratorDevice,
			Status: model.AlertStatusResolved, ResolvedAt: &now, ResolutionReason: model.AcceleratorDeviceUnavailable}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			action := evaluationAction{action: alerteval.Action{Kind: alerteval.ActionDeviceUnavailable, Event: test.event, PreviousStatus: model.AlertStatusTriggered}, state: state}
			if err := applyEvaluationAction(t.Context(), transaction, action); !errors.Is(err, alerteval.ErrConflict) {
				t.Fatalf("invalid device transition = %v", err)
			}
		})
	}
}

func TestEvaluationHealthEventSurfacesStorageFailures(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	state := model.AlertEvaluationState{RuleID: "rule", NodeID: covScalarNodeName, RuleRevision: 1}
	event := model.AlertEvaluationEvent{ID: covBlockedEventID, RuleID: state.RuleID, NodeID: state.NodeID, RuleRevision: 1,
		EpochID: covEvaluationEpoch, Kind: model.EvaluationNoData, OccurredAt: model.NowUTC(), SourceKey: "cov:blocked"}
	if _, err := transaction.ExecContext(t.Context(), `CREATE TRIGGER cov_block_evaluation_event BEFORE INSERT ON alert_evaluation_events BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, &event)); err == nil {
		t.Fatal("blocked evaluation event insert succeeded")
	}
	if _, err := transaction.ExecContext(t.Context(), "DROP TRIGGER cov_block_evaluation_event"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), `CREATE TRIGGER cov_block_evaluation_close BEFORE UPDATE ON alert_evaluation_events BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	recovered := event
	recovered.ID, recovered.Kind, recovered.ClosedAt = covRecoveredEventID, covRecoveredKind, &recovered.OccurredAt
	if err := writeEvaluationHealthEvent(t.Context(), transaction, healthActionOf(state, &recovered)); err == nil {
		t.Fatal("blocked epoch close succeeded")
	}
}

func TestApplyThresholdRecoverySurfacesMissingAndMismatchedIncidents(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	absent := event
	absent.ID = "cov-absent-incident"
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery, Event: &absent}); err == nil {
		t.Fatal("recovery of a missing incident succeeded")
	}
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err != nil || !created {
		t.Fatalf("seed incident: %v/%v", created, err)
	}
	now := model.NowUTC()
	foreign := event
	foreign.RuleID = "other-rule"
	foreign.Status, foreign.ResolvedAt, foreign.ResolutionReason = model.AlertStatusResolved, &now, covRecoveredKind
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery, Event: &foreign}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("recovery with a foreign identity = %v", err)
	}
	incomplete := event
	incomplete.Status = model.AlertStatusResolved
	if err := applyThresholdRecovery(t.Context(), transaction, alerteval.Action{Kind: alerteval.ActionThresholdRecovery, Event: &incomplete}); err == nil {
		t.Fatal("recovery without a resolution timestamp succeeded")
	}
}
