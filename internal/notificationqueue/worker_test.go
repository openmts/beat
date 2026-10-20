package notificationqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type senderFunc func(context.Context, model.NotificationDispatch) model.NotificationSendResult

func (send senderFunc) Send(ctx context.Context, dispatch model.NotificationDispatch) model.NotificationSendResult {
	return send(ctx, dispatch)
}

func TestWorkerOnlyDispatchesCommittedRowsAndStopsOnCancellation(t *testing.T) {
	s, _ := queueFixture(t)
	addQueueChannel(t, s, "channel")
	var calls atomic.Int32
	s.sender = senderFunc(func(_ context.Context, dispatch model.NotificationDispatch) model.NotificationSendResult {
		calls.Add(1)
		if dispatch.DeliveryID == "" || dispatch.Payload.Text != "PRIVATE_TEXT" {
			t.Error("dispatch did not carry decrypted message")
		}
		return model.NotificationSendResult{Success: true}
	})
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	waitReceiptState(t, s, "event", "succeeded")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if calls.Load() != 1 || s.Stats().Success != 1 {
		t.Fatal("worker retried a confirmed delivery")
	}
	snapshot, err := s.Snapshot(t.Context())
	if err != nil || snapshot.Counts.Succeeded != 1 {
		t.Fatal(err)
	}
}

func TestDisabledChannelPausesQueuedDeliveryAndCancelsInFlight(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	started := make(chan struct{})
	s.sender = senderFunc(func(ctx context.Context, _ model.NotificationDispatch) model.NotificationSendResult {
		close(started)
		<-ctx.Done()
		return model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled", Stage: "request"}
	})
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	<-started
	if _, err := s.SetEnabled(t.Context(), actor, ChannelEnabled{ID: "channel", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	waitReceiptState(t, s, "event", "pending")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if lease, err := s.claim(t.Context(), "disabled"); err != nil || lease != nil {
		t.Fatal("disabled queue continued sending")
	}
}

func waitReceiptState(t *testing.T, s *Service, key, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := s.SourceReceipt(t.Context(), key)
		if err == nil && receipt.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("receipt did not reach %s", want)
}

func TestRetentionNeverDeletesPendingOrLeasedWork(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "first")
	addQueueChannel(t, s, "second")
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	lease, err := s.claim(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.complete(t.Context(), completion{lease: *lease, result: model.NotificationSendResult{Success: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET completed_at=1 WHERE state='succeeded'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_deliveries WHERE state='pending'").Scan(&pending); err != nil || pending != 1 {
		t.Fatal("pending work erased")
	}
	var messages int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_messages").Scan(&messages); err != nil || messages != 1 {
		t.Fatal("referenced payload erased")
	}
}
