package notificationqueue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

func TestEmailChannelConfigurationEnforcesSharedEgressPolicy(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	s, actor := fixture.service, fixture.actor
	write := model.NotificationChannelWrite{Name: "SMTP", ChannelType: "email", Enabled: true, Config: json.RawMessage(`{"host":"8.8.8.8","port":465,"from":"a@example.test","to":["b@example.test"],"security":"tls"}`)}
	if _, err := s.CreateChannel(t.Context(), actor, write); err != nil {
		t.Fatal(err)
	}
	write.Config = json.RawMessage(`{"host":"127.0.0.1","port":2525,"from":"a@example.test","to":["b@example.test"],"security":"none"}`)
	if _, err := s.CreateChannel(t.Context(), actor, write); !errors.Is(err, notification.ErrProviderConfig) {
		t.Fatal("unreviewed loopback SMTP endpoint accepted")
	}
	settings := model.NotificationSettingsWrite{ExpectedRevision: 1, SuccessRetentionDays: 30, DeadRetentionDays: 90, MaximumWorkers: 8, EgressPolicy: model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{{Host: "127.0.0.1", Port: 2525, Purpose: "local SMTP fixture", ExpiresAt: s.now().Add(time.Hour)}}}}
	if _, err := s.UpdateSettings(t.Context(), actor, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel(t.Context(), actor, write); err != nil {
		t.Fatal("reviewed SMTP endpoint rejected")
	}
	write.Enabled, write.Config = false, json.RawMessage(`{}`)
	if _, err := s.CreateChannel(t.Context(), actor, write); err != nil {
		t.Fatal("disabled incomplete SMTP draft rejected")
	}
	if err := s.validateChannelEgress(t.Context(), model.NotificationEgressPolicy{}, json.RawMessage(`invalid`)); err == nil {
		t.Fatal("egress validated malformed config")
	}
}

func TestQueueConstructorRejectsInvalidPoolAndUnreadySchema(t *testing.T) {
	s, db := queueFixture(t)
	for _, workers := range []int{-1, 33} {
		if _, err := NewService(Config{Database: db.DB, Secrets: s.secrets, Workers: workers}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("invalid worker pool accepted")
		}
	}
	if err := s.Run(t.Context()); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("worker started without a sender")
	}
	if _, err := db.DB.Exec("UPDATE notification_secret_state SET phase='sanitize'"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(Config{Database: db.DB, Secrets: s.secrets}); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("notification queue started before secret sanitization")
	}
}

func TestSnapshotReadsAllDeliveryStatesAndOpenCircuits(t *testing.T) {
	s, db := queueFixture(t)
	for _, id := range []string{"one", "two", "three", "four", "five"} {
		addQueueChannel(t, s, id)
	}
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("states"), nil)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.claim(t.Context(), "worker")
	if err != nil || lease == nil {
		t.Fatal(err)
	}
	states := []string{"pending", "succeeded", "dead", "cancelled"}
	index := 0
	for _, delivery := range receipt.Deliveries {
		if delivery.ID == lease.ID {
			continue
		}
		if _, err := db.DB.Exec("UPDATE notification_deliveries SET state=? WHERE id=?", states[index], delivery.ID); err != nil {
			t.Fatal(err)
		}
		index++
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET created_at=?", s.now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_channel_health SET circuit='open',open_until=? WHERE channel_id='one'", s.now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(t.Context())
	if err != nil || snapshot.OpenCircuits != 1 || snapshot.OldestPendingAgeSeconds < 60 {
		t.Fatal("queue operational snapshot lost pressure information")
	}
	if snapshot.Counts.Pending != 1 || snapshot.Counts.Leased != 1 || snapshot.Counts.Succeeded != 1 || snapshot.Counts.Dead != 1 || snapshot.Counts.Cancelled != 1 {
		t.Fatal("snapshot omitted a delivery state")
	}
	if _, err := db.DB.Exec("ALTER TABLE notification_channel_health RENAME COLUMN open_until TO missing_until"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(t.Context()); err == nil {
		t.Fatal("snapshot ignored missing health state")
	}
}
