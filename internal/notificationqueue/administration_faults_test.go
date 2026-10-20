package notificationqueue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAdministrativeMutationsRollbackStorageFailures(t *testing.T) {
	cases := []struct {
		name, statement string
		operation       func(*testing.T, queueFaultFixture) error
	}{
		{"disable", "CREATE TRIGGER fail_disable BEFORE UPDATE ON alert_channels BEGIN SELECT RAISE(IGNORE); END", func(t *testing.T, f queueFaultFixture) error {
			_, err := f.service.SetEnabled(t.Context(), f.actor, ChannelEnabled{ID: "channel"})
			return err
		}},
		{"delete_attempt", "CREATE TRIGGER fail_cancel BEFORE UPDATE ON notification_delivery_attempts BEGIN SELECT RAISE(ABORT,'injected'); END", func(t *testing.T, f queueFaultFixture) error {
			return f.service.DeleteChannel(t.Context(), f.actor, "channel")
		}},
		{"cancel_attempt", "CREATE TRIGGER fail_cancel BEFORE UPDATE ON notification_delivery_attempts BEGIN SELECT RAISE(ABORT,'injected'); END", func(t *testing.T, f queueFaultFixture) error {
			return f.service.CancelDelivery(t.Context(), f.actor, f.receipt.Deliveries[0].ID)
		}},
		{"cancel_delivery", "CREATE TRIGGER fail_cancel BEFORE UPDATE ON notification_deliveries BEGIN SELECT RAISE(IGNORE); END", func(t *testing.T, f queueFaultFixture) error {
			return f.service.CancelDelivery(t.Context(), f.actor, f.receipt.Deliveries[0].ID)
		}},
		{"test_budget", "CREATE TRIGGER fail_budget BEFORE INSERT ON notification_request_budgets BEGIN SELECT RAISE(ABORT,'injected'); END", func(t *testing.T, f queueFaultFixture) error {
			_, err := f.service.TestChannel(t.Context(), f.actor, "channel")
			return err
		}},
		{"test_delivery", "CREATE TRIGGER fail_delivery BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT,'injected'); END", func(t *testing.T, f queueFaultFixture) error {
			_, err := f.service.TestChannel(t.Context(), f.actor, "channel")
			return err
		}},
		{"settings", "CREATE TRIGGER fail_settings BEFORE UPDATE ON notification_settings BEGIN SELECT RAISE(ABORT,'injected'); END", func(t *testing.T, f queueFaultFixture) error {
			_, err := f.service.UpdateSettings(t.Context(), f.actor, model.NotificationSettingsWrite{ExpectedRevision: 1, SuccessRetentionDays: 30, DeadRetentionDays: 90, MaximumWorkers: 8})
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			_ = fixture.lease(t)
			fixture.sql(t, test.statement)
			if err := test.operation(t, fixture); err == nil {
				t.Fatal("administrative storage failure was ignored")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_delivery_attempts WHERE outcome='in_progress'", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM admin_audit_events", 0)
		})
	}
}

func TestPreviewUsesStoredRevisionAndEnforcesActorBudget(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	now := fixture.service.now()
	fixture.service.now = func() time.Time { return now }
	input := PreviewWrite{ChannelID: "channel", ExpectedRevision: 1, ChannelType: "webhook", Config: json.RawMessage(`{}`)}
	for range 30 {
		if _, err := fixture.service.Preview(t.Context(), fixture.actor, input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.service.Preview(t.Context(), fixture.actor, input); !errors.Is(err, ErrRateLimited) {
		t.Fatal("preview rate limit was not persisted")
	}
	if _, err := fixture.service.Preview(t.Context(), Actor{}, input); !errors.Is(err, ErrForbidden) {
		t.Fatal("anonymous preview accepted")
	}
}

func TestPreviewRejectsBadPayloadConfigRevisionAndAudit(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	for _, input := range []PreviewWrite{
		{Data: map[string]string{"unknown": "value"}},
		{ChannelType: "webhook", Config: json.RawMessage(`{"endpoint":false}`)},
		{ChannelID: "channel", ExpectedRevision: 99, ChannelType: "webhook"},
		{ChannelType: "webhook", Config: json.RawMessage(`{"endpoint":"https://8.8.8.8"}`), TemplateText: "{{unknown}}"},
	} {
		if _, err := fixture.service.Preview(t.Context(), fixture.actor, input); err == nil {
			t.Fatal("invalid preview accepted")
		}
	}
	fixture.sql(t, "CREATE TRIGGER fail_preview_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END")
	input := PreviewWrite{ChannelType: "webhook", Config: json.RawMessage(`{"endpoint":"https://8.8.8.8"}`)}
	if _, err := fixture.service.Preview(t.Context(), fixture.actor, input); err == nil {
		t.Fatal("preview audit failure ignored")
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_request_budgets", 0)
}
