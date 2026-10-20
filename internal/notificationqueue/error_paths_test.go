package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAdministrativeOperationsRejectMissingRecordsAndStaleProof(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	for _, operation := range []func() error{
		func() error { _, err := s.GetChannel(t.Context(), "missing"); return err }, func() error { _, err := s.GetDelivery(t.Context(), "missing"); return err },
		func() error { _, err := s.TestChannel(t.Context(), actor, "missing"); return err }, func() error { return s.CancelDelivery(t.Context(), actor, "missing") },
		func() error { return s.DeleteChannel(t.Context(), actor, "missing") }, func() error {
			_, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: "missing", ConfirmDuplicate: true})
			return err
		},
	} {
		if err := operation(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing record returned %v", err)
		}
	}
	addQueueChannel(t, s, "channel")
	for _, statement := range []string{"UPDATE admin_sessions SET reauthenticated_until=NULL", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP", "UPDATE admin_users SET enabled=0"} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteChannel(t.Context(), actor, "channel"); err == nil {
			t.Fatal("stale actor mutated channel")
		}
	}
	if _, err := s.TestChannel(t.Context(), Actor{}, "channel"); !errors.Is(err, ErrForbidden) {
		t.Fatal("empty actor accepted")
	}
}

func TestQueueClosedDatabaseFailsWithoutCallingBusinessMutation(t *testing.T) {
	s, db := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), func(*sql.Tx) error { called = true; return nil }); err == nil || called {
		t.Fatal("closed queue invoked mutation")
	}
	for name, operation := range map[string]func() error{
		"ready": func() error { return s.Ready(t.Context()) }, "list_channels": func() error { _, err := s.ListChannels(t.Context()); return err },
		"get_channel": func() error { _, err := s.GetChannel(t.Context(), "channel"); return err }, "history": func() error { _, err := s.ListDeliveries(t.Context(), DeliveryFilter{}); return err },
		"detail": func() error { _, err := s.GetDelivery(t.Context(), "delivery"); return err }, "settings": func() error { _, err := s.Settings(t.Context()); return err },
		"test": func() error { _, err := s.TestChannel(t.Context(), actor, "channel"); return err }, "delete": func() error { return s.DeleteChannel(t.Context(), actor, "channel") },
		"retry": func() error {
			_, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: "delivery", ConfirmDuplicate: true})
			return err
		},
		"cancel": func() error { return s.CancelDelivery(t.Context(), actor, "delivery") }, "claim": func() error { _, err := s.claim(t.Context(), "worker"); return err },
		"complete": func() error { return s.complete(t.Context(), completion{}) }, "prune": func() error { return s.Prune(t.Context()) }, "snapshot": func() error { _, err := s.Snapshot(t.Context()); return err },
		"preview": func() error { _, err := s.Preview(t.Context(), actor, PreviewWrite{}); return err },
	} {
		if err := operation(); err == nil {
			t.Fatalf("%s ignored unavailable persistence", name)
		}
	}
}

func TestChannelAndSettingsAuditIgnoreRollsBackWrites(t *testing.T) {
	s, db := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("CREATE TRIGGER ignore_notification_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled(t.Context(), actor, ChannelEnabled{ID: "channel", Enabled: false}); err == nil {
		t.Fatal("audit ignore allowed disable")
	}
	if err := s.DeleteChannel(t.Context(), actor, "channel"); err == nil {
		t.Fatal("audit ignore allowed delete")
	}
	if err := s.CancelDelivery(t.Context(), actor, receipt.Deliveries[0].ID); err == nil {
		t.Fatal("audit ignore allowed cancellation")
	}
	settings, err := s.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettings(t.Context(), actor, model.NotificationSettingsWrite{ExpectedRevision: settings.Revision, SuccessRetentionDays: 40, DeadRetentionDays: 90, MaximumWorkers: 8}); err == nil {
		t.Fatal("audit ignore changed settings")
	}
	channel, err := s.GetChannel(t.Context(), "channel")
	if err != nil || !channel.Enabled {
		t.Fatal("failed audit changed channel")
	}
}

func TestValidationLimitsAndSenderPanicStayBounded(t *testing.T) {
	if _, err := NewService(Config{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	for _, filter := range []DeliveryFilter{{Limit: 101}, {Offset: -1}, {State: "invalid"}} {
		if _, err := s.ListDeliveries(t.Context(), filter); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(err)
		}
	}
	for _, write := range []model.NotificationChannelWrite{{}, {Name: "Bad", ChannelType: "webhook", EventKinds: []string{"security.login_unknown"}},
		{Name: "Bad", ChannelType: "webhook", TemplateText: "{{env.HOME}}"}, {Name: "Bad", ChannelType: "webhook", Config: json.RawMessage(`invalid`)}} {
		if _, err := s.CreateChannel(t.Context(), actor, write); err == nil {
			t.Fatal("invalid channel accepted")
		}
	}
	if _, err := s.UpdateSettings(t.Context(), actor, model.NotificationSettingsWrite{}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	if _, err := s.retryDelay(0, 0); err == nil {
		t.Fatal("invalid attempt accepted")
	}
	delay, err := s.retryDelay(1, time.Hour*2)
	if err != nil || delay > time.Hour+6*time.Minute {
		t.Fatal("Retry-After unbounded")
	}
	s.sender = senderFunc(func(context.Context, model.NotificationDispatch) model.NotificationSendResult { panic("PRIVATE_PANIC") })
	result := s.sendSafely(t.Context(), model.NotificationDispatch{})
	if result.ErrorCode != "sender_failed" || result.Success {
		t.Fatal("sender panic escaped")
	}
}
