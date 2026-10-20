package store

import (
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAlertTransactionRejectsInvalidInputAndClosedTransactions(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	if _, err := events.CreateActiveEventTx(t.Context(), nil, &event); err == nil {
		t.Fatal("nil transaction accepted")
	}
	if _, err := events.CreateActiveEventTx(t.Context(), transaction, nil); err == nil {
		t.Fatal("nil event accepted")
	}
	for _, change := range []func(*model.AlertEvent){
		func(value *model.AlertEvent) { value.ID = "" }, func(value *model.AlertEvent) {
			value.TriggeredAt = time.Time{}
		},
		func(value *model.AlertEvent) { value.Status = model.AlertStatusResolved }, func(value *model.AlertEvent) { value.Value = math.NaN() }, func(value *model.AlertEvent) { value.Value = math.Inf(1) },
	} {
		invalid := event
		change(&invalid)
		if _, err := events.CreateActiveEventTx(t.Context(), transaction, &invalid); !errors.Is(err, ErrInvalidAlertTransition) {
			t.Fatalf("invalid event accepted: %v", err)
		}
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err == nil {
		t.Fatal("closed transaction accepted")
	}
}

func TestAlertTransitionValidationAndIgnoredWrites(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	if err := events.CreateEvent(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	valid := AlertEventTransition{Event: event, Previous: model.AlertStatusTriggered}
	valid.Event.Status, valid.Event.AcknowledgedAt = model.AlertStatusAcknowledged, &now
	transaction := alertTestTx(t, database.DB)
	if _, err := events.TransitionEventTx(t.Context(), nil, valid); err == nil {
		t.Fatal("nil transition transaction")
	}
	for _, change := range []func(*AlertEventTransition){
		func(value *AlertEventTransition) { value.Event.NodeID = "" }, func(value *AlertEventTransition) { value.Previous = model.AlertStatusResolved },
		func(value *AlertEventTransition) { value.Event.AcknowledgedAt = nil }, func(value *AlertEventTransition) { value.Event.Status = model.AlertStatusTriggered },
		func(value *AlertEventTransition) { value.Event.Status = model.AlertStatusResolved },
	} {
		invalid := valid
		change(&invalid)
		if _, err := events.TransitionEventTx(t.Context(), transaction, invalid); !errors.Is(err, ErrInvalidAlertTransition) {
			t.Fatalf("invalid transition accepted: %v", err)
		}
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER ignore_alert_update BEFORE UPDATE ON alert_events BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	transaction = alertTestTx(t, database.DB)
	if changed, err := events.TransitionEventTx(t.Context(), transaction, valid); err == nil || changed {
		t.Fatal("ignored transition claimed success")
	}
}

func TestAlertTransitionAuditFailureRollsBackAcknowledgement(t *testing.T) {
	fixture := newDashboardFixture(t)
	events := NewAlertEventStore(fixture.database.DB)
	event := model.AlertEvent{ID: "event", RuleID: "rule", NodeID: "node", Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC()}
	if err := events.CreateEvent(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB.Exec(`CREATE TRIGGER fail_event_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END`); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	event.Status, event.AcknowledgedAt = model.AlertStatusAcknowledged, &now
	actor := EventMutationActor{UserID: fixture.actor.UserID, SessionID: fixture.actor.SessionID}
	transaction := alertTestTx(t, fixture.database.DB)
	if _, err := events.TransitionEventTx(t.Context(), transaction, AlertEventTransition{Event: event, Previous: model.AlertStatusTriggered, Actor: &actor}); err == nil {
		t.Fatal("audit failure ignored")
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	stored, err := events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored.Status != model.AlertStatusTriggered {
		t.Fatal("failed audit retained acknowledgement")
	}
}

type failingEventResult struct {
	count int64
	err   error
}

func (result failingEventResult) LastInsertId() (int64, error) { return 0, nil }

func (result failingEventResult) RowsAffected() (int64, error) { return result.count, result.err }

func TestAlertWritesRejectUnexpectedAffectedRows(t *testing.T) {
	for _, result := range []sql.Result{failingEventResult{count: -1}, failingEventResult{count: 2}, failingEventResult{err: errors.New("result unavailable")}} {
		if _, err := eventMutationResult(result, nil); err == nil {
			t.Fatal("unexpected write result accepted")
		}
	}
	if _, err := eventMutationResult(nil, errors.New("write failed")); err == nil {
		t.Fatal("write error lost")
	}
}
