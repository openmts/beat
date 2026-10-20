package notificationqueue

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetentionKeepsSuccessfulRetryWhileParentEvidenceIsRetained(t *testing.T) {
	s, db := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := s.now()
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='dead',completed_at=?", now.Add(-45*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	retry, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: receipt.Deliveries[0].ID, ConfirmDuplicate: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='succeeded',completed_at=? WHERE id=?", now.Add(-31*24*time.Hour).UnixMilli(), retry.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || current.State != "succeeded" || current.Deliveries[0].ID != retry.ID {
		t.Fatal("retention reverted a successful retry to its old dead ancestor")
	}
	s.now = func() time.Time { return now.Add(100 * 24 * time.Hour) }
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SourceReceipt(t.Context(), "event"); !errors.Is(err, ErrNotFound) {
		t.Fatal("fully expired retry chain was not removed atomically")
	}
}

func TestRetentionPrunesLongRetryChainInBoundedTransactions(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='dead',completed_at=1"); err != nil {
		t.Fatal(err)
	}
	previous := receipt.Deliveries[0].ID
	for index := range maximumPrunedDeliveries + 2 {
		id := fmt.Sprintf("retry-%03d", index)
		state := "dead"
		if index == maximumPrunedDeliveries+1 {
			state = "succeeded"
		}
		_, err := db.DB.Exec(`INSERT INTO notification_deliveries(id,message_id,channel_id,channel_revision,state,next_attempt_at,retry_of,created_at,updated_at,completed_at)
			VALUES(?,?,'channel',1,?,1,?,1,1,1)`, id, receipt.MessageID, state, previous)
		if err != nil {
			t.Fatal(err)
		}
		previous = id
	}
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || current.State != "succeeded" || current.Deliveries[0].ID != previous {
		t.Fatal("bounded prune lost the retry outcome")
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_deliveries").Scan(&count); err != nil || count != 3 {
		t.Fatal("prune exceeded or failed its transaction budget")
	}
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SourceReceipt(t.Context(), "event"); !errors.Is(err, ErrNotFound) {
		t.Fatal("long retry chain could not be fully pruned")
	}
	rows, err := db.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatal("retention left a dangling retry reference")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
