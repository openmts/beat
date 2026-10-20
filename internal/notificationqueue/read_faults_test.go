package notificationqueue

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestNotificationReadsRejectMalformedStoredData(t *testing.T) {
	cases := []struct {
		name, statement string
		read            func(queueFaultFixture) error
	}{
		{"channel_kinds", "UPDATE alert_channels SET event_kinds_json='not-json'", func(f queueFaultFixture) error {
			_, err := f.service.GetChannel(context.Background(), "channel")
			return err
		}},
		{"channel_public", "UPDATE alert_channel_revisions SET public_config_json='not-json'", func(f queueFaultFixture) error { _, err := f.service.ListChannels(context.Background()); return err }},
		{"channel_presence", "UPDATE alert_channel_revisions SET secret_present_json='not-json'", func(f queueFaultFixture) error {
			_, err := f.service.GetChannel(context.Background(), "channel")
			return err
		}},
		{"delivery_timestamp", "UPDATE notification_deliveries SET next_attempt_at='not-time'", func(f queueFaultFixture) error {
			_, err := f.service.ListDeliveries(context.Background(), DeliveryFilter{})
			return err
		}},
		{"attempt_timestamp", "UPDATE notification_delivery_attempts SET started_at='not-time'", func(f queueFaultFixture) error {
			_, err := f.service.GetDelivery(context.Background(), f.receipt.Deliveries[0].ID)
			return err
		}},
		{"attempt_schema", "ALTER TABLE notification_delivery_attempts RENAME COLUMN outcome TO missing_outcome", func(f queueFaultFixture) error {
			_, err := f.service.GetDelivery(context.Background(), f.receipt.Deliveries[0].ID)
			return err
		}},
		{"settings_policy", "UPDATE notification_settings SET egress_policy_json='not-json'", func(f queueFaultFixture) error { _, err := f.service.Settings(context.Background()); return err }},
		{"receipt_schema", "ALTER TABLE notification_deliveries RENAME COLUMN attempt_count TO missing_attempt", func(f queueFaultFixture) error {
			_, err := f.service.SourceReceipt(context.Background(), "seed")
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			_ = fixture.lease(t)
			fixture.sql(t, test.statement)
			if err := test.read(fixture); err == nil {
				t.Fatal("malformed persisted data was presented as healthy")
			}
		})
	}
}

func TestDispatchRejectsTamperedSecretsAndErasesPartialPlaintext(t *testing.T) {
	cases := []string{
		"UPDATE alert_channel_revisions SET secret_ciphertext=zeroblob(length(secret_ciphertext))",
		"UPDATE notification_messages SET payload_encrypted=zeroblob(length(payload_encrypted))",
		"UPDATE notification_settings SET egress_policy_json='not-json'",
		"ALTER TABLE notification_messages RENAME COLUMN payload_encrypted TO missing_payload",
	}
	for _, statement := range cases {
		t.Run(statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			lease := fixture.lease(t)
			fixture.sql(t, statement)
			dispatch, err := fixture.service.readDispatch(t.Context(), lease)
			if err == nil || len(dispatch.Config) != 0 || dispatch.Payload.Text != "" {
				t.Fatal("dispatch ignored tampering or returned partially decrypted input")
			}
		})
	}
}

func TestDispatchRejectsAuthenticatedButInvalidPayload(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	lease := fixture.lease(t)
	err := fixture.service.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		sealed, err := access.Seal([]byte(`{"text":"PRIVATE_PARTIAL","data":[]}`), secretbox.NotificationMessageAAD(fixture.receipt.MessageID))
		if err != nil {
			return err
		}
		_, err = fixture.db.Exec("UPDATE notification_messages SET payload_encrypted=?", sealed)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := fixture.service.readDispatch(t.Context(), lease)
	if err == nil || dispatch.Payload.Text != "" || len(dispatch.Config) != 0 {
		t.Fatal("invalid payload escaped the decryption boundary")
	}
}

func TestDispatchRechecksLeaseAndPauseState(t *testing.T) {
	for _, statement := range []string{
		"UPDATE alert_channels SET enabled=0",
		"UPDATE alert_channels SET deleted_at=CURRENT_TIMESTAMP",
		"UPDATE notification_deliveries SET lease_token_hash=zeroblob(32)",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			lease := fixture.lease(t)
			fixture.sql(t, statement)
			if _, err := fixture.service.readDispatch(t.Context(), lease); err == nil {
				t.Fatal("dispatch used a revoked lease or disabled channel")
			}
		})
	}
}
