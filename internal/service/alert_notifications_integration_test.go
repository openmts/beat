package service

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestAlertProducerRealQueueSharesFailureRecoveryAndDeduplication(t *testing.T) {
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: database.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	service := NewAlertNotificationService(store.NewAlertEventStore(database.DB), queue)
	event := model.AlertEvent{ID: uuid.NewString(), RuleID: "rule", NodeID: "node", Message: "private alert message", Value: 90,
		Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC()}
	if _, err := database.DB.Exec(`CREATE TRIGGER reject_outbox BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'outbox failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Trigger(t.Context(), &event); err == nil {
		t.Fatal("outbox failure reported success")
	}
	stored, err := service.events.GetEventByID(t.Context(), event.ID)
	if err != nil || stored != nil {
		t.Fatal("outbox failure retained event")
	}
	var audits int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE resource_id=?", event.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatal("outbox failure retained success audit")
	}
	if _, err := database.DB.Exec("DROP TRIGGER reject_outbox"); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Trigger(t.Context(), &event)
	if err != nil || receipt.State != "not_routed" {
		t.Fatalf("unrouted source: %+v/%v", receipt, err)
	}
	duplicate, err := service.Trigger(t.Context(), &event)
	if err != nil || !duplicate.Duplicate || duplicate.MessageID != receipt.MessageID {
		t.Fatal("lost acknowledgement duplicated source")
	}
	var encrypted []byte
	if err := database.DB.QueryRow("SELECT payload_encrypted FROM notification_messages WHERE id=?", receipt.MessageID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if !secretbox.IsV2Envelope(encrypted) || bytes.Contains(encrypted, []byte(event.Message)) {
		t.Fatal("notification payload was not encrypted")
	}
}
