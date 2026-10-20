package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func newTransactionalAlert(t *testing.T) (*SQLiteStore, *AlertEventStore, model.AlertEvent) {
	t.Helper()
	database := setupTestDB(t)
	event := model.AlertEvent{ID: uuid.NewString(), RuleID: "rule", NodeID: "node", Message: "threshold", Value: 90, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC()}
	return database, NewAlertEventStore(database.DB), event
}

func alertTestTx(t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	transaction, err := beginWriteTx(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	return transaction
}

func TestAlertTransactionRollbackRemovesEventAndAudit(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	created, err := events.CreateActiveEventTx(t.Context(), transaction, &event)
	if err != nil || !created {
		t.Fatalf("create event: %v/%v", created, err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	stored, err := events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored != nil {
		t.Fatal("rolled-back event remains")
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE resource_id=?", event.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rolled-back event retained a success audit")
	}
}

func TestAlertTransactionDeduplicatesActiveEpisode(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	transaction := alertTestTx(t, database.DB)
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err != nil || !created {
		t.Fatal(err)
	}
	event.ID = uuid.NewString()
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err != nil || created {
		t.Fatalf("duplicate episode: %v/%v", created, err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	stored, err := events.ListAlertEvents(t.Context())
	if err != nil || len(stored) != 1 {
		t.Fatal("duplicate active alerts persisted")
	}
}

func TestAlertTransactionRejectsIgnoredDatabaseWrite(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	if _, err := database.DB.Exec(`CREATE TRIGGER ignore_alert_insert BEFORE INSERT ON alert_events BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, database.DB)
	if created, err := events.CreateActiveEventTx(t.Context(), transaction, &event); err == nil || created {
		t.Fatal("ignored event insert looked successful")
	}
}

func TestAlertTransitionCompareAndSwapAndAudit(t *testing.T) {
	database, events, event := newTransactionalAlert(t)
	if err := events.CreateEvent(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	event.Status, event.AcknowledgedAt = model.AlertStatusAcknowledged, &now
	transaction := alertTestTx(t, database.DB)
	change := AlertEventTransition{Event: event, Previous: model.AlertStatusTriggered}
	if updated, err := events.TransitionEventTx(t.Context(), transaction, change); err != nil || !updated {
		t.Fatal(err)
	}
	if updated, err := events.TransitionEventTx(t.Context(), transaction, change); err != nil || updated {
		t.Fatal("stale transition overwrote current status")
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	stored, err := events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored.Status != model.AlertStatusAcknowledged {
		t.Fatal("acknowledgement not committed")
	}
}

func TestAlertTransitionRejectsRevokedActor(t *testing.T) {
	fixture := newDashboardFixture(t)
	events := NewAlertEventStore(fixture.database.DB)
	event := model.AlertEvent{ID: uuid.NewString(), RuleID: "rule", NodeID: "node", Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC()}
	if err := events.CreateEvent(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	if err := fixture.admins.RevokeSession(t.Context(), fixture.actor.SessionID, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	event.Status, event.AcknowledgedAt = model.AlertStatusAcknowledged, &now
	actor := EventMutationActor{UserID: fixture.actor.UserID, SessionID: fixture.actor.SessionID}
	transaction := alertTestTx(t, fixture.database.DB)
	_, err := events.TransitionEventTx(t.Context(), transaction, AlertEventTransition{Event: event, Previous: model.AlertStatusTriggered, Actor: &actor})
	if !errors.Is(err, ErrAdminSessionNotFound) {
		t.Fatalf("revoked actor mutation: %v", err)
	}
}
