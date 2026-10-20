package notificationqueue

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestReconciliationRepairsMissingFailureAndAvoidsRecursion(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "failing")
	addQueueChannel(t, s, "observer")
	if _, err := db.DB.Exec(`UPDATE alert_channels SET event_kinds_json='["notification.delivery_failed"]' WHERE id='observer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	completed := s.now().Add(-time.Hour)
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='dead',completed_at=?,error_code='lease_expired'", completed.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ReconcileFailures(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	lease, err := s.claim(t.Context(), "observer")
	if err != nil || lease == nil || lease.ChannelID != "observer" || lease.EventKind != "notification.delivery_failed" {
		t.Fatal("reconciliation failed to route the durable failure")
	}
	if err := s.complete(t.Context(), completion{lease: *lease, result: model.NotificationSendResult{ErrorCode: "http_rejected"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.maintain(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='notification.delivery_failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("failure message duplicated or recursed")
	}
}

func TestExpiredFinalLeaseGeneratesSingleFailureMessage(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "channel")
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET attempt_count=5"); err != nil {
		t.Fatal(err)
	}
	if lease, err := s.claim(t.Context(), "worker"); err != nil || lease == nil {
		t.Fatal(err)
	}
	now := s.now().Add(deliveryLeaseLifetime + time.Second)
	s.now = func() time.Time { return now }
	if lease, err := s.claim(t.Context(), "recovery"); err != nil || lease != nil {
		t.Fatal("final expired lease remained dispatchable")
	}
	if err := s.ReconcileFailures(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='notification.delivery_failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("final expired lease lost or duplicated its failure notification")
	}
}
