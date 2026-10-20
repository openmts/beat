package notificationqueue

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/model"
)

func fillPendingQueue(t *testing.T, fixture queueFaultFixture) {
	t.Helper()
	_, err := fixture.db.Exec(`WITH RECURSIVE numbered(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbered WHERE n<?)
		INSERT INTO notification_deliveries(id,message_id,channel_id,channel_revision,state,next_attempt_at,retry_of,created_at,updated_at)
		SELECT 'filler-'||n,?,'channel',1,'pending',1,CASE WHEN n=1 THEN ? ELSE 'filler-'||(n-1) END,1,1 FROM numbered`,
		MaximumPendingDeliveries-1, fixture.receipt.MessageID, fixture.receipt.Deliveries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFullQueueRejectsNewSourceWithoutLosingBusinessAtomicity(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	fillPendingQueue(t, fixture)
	fixture.sql(t, "CREATE TABLE business_source(id TEXT PRIMARY KEY)")
	_, err := fixture.service.Enqueue(t.Context(), enqueueRequest("over-capacity"), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO business_source VALUES('source')")
		return err
	})
	if !errors.Is(err, ErrQueueCapacity) {
		t.Fatalf("expected queue capacity error, got %v", err)
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM business_source", 0)
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='pending'", MaximumPendingDeliveries)
}

func TestFailureAtCapacityCommitsTerminalEvidenceAndReconcilesLater(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	lease := fixture.lease(t)
	for _, id := range []string{"observer-a", "observer-b"} {
		addQueueChannel(t, fixture.service, id)
	}
	fixture.sql(t, `UPDATE alert_channels SET event_kinds_json='["notification.delivery_failed"]' WHERE id<>'channel'`)
	fillPendingQueue(t, fixture)
	result := model.NotificationSendResult{ErrorCode: "provider_rejected", Stage: "response"}
	if err := fixture.service.complete(t.Context(), completion{lease: lease, result: result}); err != nil {
		t.Fatal("full queue prevented a failed attempt from becoming terminal")
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='dead'", 1)
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_messages WHERE event_kind='notification.delivery_failed'", 0)
	if err := fixture.service.ReconcileFailures(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.sql(t, "UPDATE notification_deliveries SET state='cancelled' WHERE state='pending'")
	if err := fixture.service.ReconcileFailures(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_messages WHERE event_kind='notification.delivery_failed'", 1)
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='pending'", 2)
}

func TestChannelLimitIsCheckedWithinCommitTransaction(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	for index := range MaximumChannels {
		addQueueChannel(t, s, fmt.Sprintf("channel-%d", index))
	}
	write := model.NotificationChannelWrite{Name: "Overflow", ChannelType: "webhook", Enabled: true, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8"}`)}
	if _, err := s.CreateChannel(t.Context(), actor, write); !errors.Is(err, ErrQueueCapacity) {
		t.Fatal("channel capacity was not enforced")
	}
}
