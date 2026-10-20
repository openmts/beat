package notificationqueue

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestLeaseExcludesCompetingWorkersAndRejectsStaleCompletion(t *testing.T) {
	s, _ := queueFixture(t)
	addQueueChannel(t, s, "channel")
	if _, err := s.Enqueue(t.Context(), enqueueRequest("first"), nil); err != nil {
		t.Fatal(err)
	}
	first, err := s.claim(t.Context(), "worker-one")
	if err != nil || first == nil {
		t.Fatal(err)
	}
	if second, err := s.claim(t.Context(), "worker-two"); err != nil || second != nil {
		t.Fatal("channel single-flight violated")
	}
	now := s.now().Add(deliveryLeaseLifetime + time.Second)
	s.now = func() time.Time { return now }
	second, err := s.claim(t.Context(), "worker-two")
	if err != nil || second == nil || second.Attempt != 2 {
		t.Fatal("expired lease not recovered")
	}
	if err := s.complete(t.Context(), completion{lease: *first, result: model.NotificationSendResult{Success: true}}); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale token completed recovered delivery")
	}
	if err := s.complete(t.Context(), completion{lease: *second, result: model.NotificationSendResult{Success: true}}); err != nil {
		t.Fatal(err)
	}
	if third, err := s.claim(t.Context(), "worker-three"); err != nil || third != nil {
		t.Fatal("confirmed success was retried")
	}
	receipt, err := s.SourceReceipt(t.Context(), "first")
	if err != nil || receipt.State != "succeeded" || receipt.Deliveries[0].AttemptCount != 2 {
		t.Fatal(err)
	}
}

func TestRetryBudgetCircuitAndFailureMessageAreDurable(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "channel")
	now := s.now()
	s.now = func() time.Time { return now }
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 6; attempt++ {
		lease, err := s.claim(t.Context(), "worker")
		if err != nil || lease == nil {
			t.Fatalf("claim attempt %d: %v", attempt, err)
		}
		if err := s.complete(t.Context(), completion{lease: *lease, result: model.NotificationSendResult{Retryable: true, ErrorCode: "http_rejected", Stage: "response", StatusClass: "5xx"}}); err != nil {
			t.Fatal(err)
		}
		if attempt == 5 {
			var circuit string
			if err := db.DB.QueryRow("SELECT circuit FROM notification_channel_health WHERE channel_id='channel'").Scan(&circuit); err != nil || circuit != "open" {
				t.Fatal("circuit did not open")
			}
		}
		now = now.Add(time.Hour)
	}
	if lease, err := s.claim(t.Context(), "worker"); err != nil || lease != nil {
		t.Fatal("seventh automatic attempt allowed")
	}
	receipt, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || receipt.State != "dead" || receipt.Deliveries[0].AttemptCount != 6 {
		t.Fatal("retry budget not terminal")
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='notification.delivery_failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("failure notification missing")
	}
}

func TestFinalExpiredLeaseCommitsRecoveryWithoutAnyDueWork(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET attempt_count=5 WHERE id=?", receipt.Deliveries[0].ID); err != nil {
		t.Fatal(err)
	}
	lease, err := s.claim(t.Context(), "worker")
	if err != nil || lease == nil {
		t.Fatal(err)
	}
	now := s.now().Add(deliveryLeaseLifetime + time.Second)
	s.now = func() time.Time { return now }
	if next, err := s.claim(t.Context(), "recovery"); err != nil || next != nil {
		t.Fatal(err)
	}
	recovered, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || recovered.State != "dead" {
		t.Fatal("terminal recovery rolled back")
	}
}
