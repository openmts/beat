package notificationqueue

import (
	"database/sql"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestEnqueueRollsBackSourceOnEveryOutboxBoundaryFailure(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, queueFaultFixture)
	}{
		{"delivery_insert", func(t *testing.T, f queueFaultFixture) {
			f.sql(t, "CREATE TRIGGER reject_delivery BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT,'injected'); END")
		}},
		{"message_ignore", func(t *testing.T, f queueFaultFixture) {
			f.sql(t, "CREATE TRIGGER ignore_message BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(IGNORE); END")
		}},
		{"subscription_json", func(t *testing.T, f queueFaultFixture) { f.sql(t, "UPDATE alert_channels SET event_kinds_json='bad'") }},
		{"retention_read", func(t *testing.T, f queueFaultFixture) {
			f.sql(t, "ALTER TABLE notification_settings RENAME COLUMN dead_retention_days TO missing_retention")
		}},
		{"entropy", func(_ *testing.T, f queueFaultFixture) { f.service.random = unavailableEntropy{} }},
		{"seal", func(_ *testing.T, f queueFaultFixture) {
			f.service.secrets = failingResourceService{base: f.service.secrets, seal: true}
		}},
		{"commit", injectOutboxCommitFailure},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			fixture.sql(t, "CREATE TABLE business_source(id TEXT PRIMARY KEY)")
			test.setup(t, fixture)
			_, err := fixture.service.Enqueue(t.Context(), enqueueRequest("new-source"), func(tx *sql.Tx) error {
				_, err := tx.Exec("INSERT INTO business_source VALUES('source')")
				return err
			})
			if err == nil {
				t.Fatal("outbox failure was ignored")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM business_source", 0)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_messages", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries", 1)
		})
	}
}

func injectOutboxCommitFailure(t *testing.T, fixture queueFaultFixture) {
	t.Helper()
	fixture.sql(t, "CREATE TABLE fault_parent(id TEXT PRIMARY KEY)",
		"CREATE TABLE fault_child(parent TEXT REFERENCES fault_parent(id) DEFERRABLE INITIALLY DEFERRED)",
		"CREATE TRIGGER fail_commit AFTER INSERT ON notification_messages BEGIN INSERT INTO fault_child VALUES('missing'); END")
}

func TestClaimWritesLeaseAndAttemptAtomically(t *testing.T) {
	for _, trigger := range []string{
		"CREATE TRIGGER fail_claim BEFORE UPDATE ON notification_deliveries BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER fail_attempt BEFORE INSERT ON notification_delivery_attempts BEGIN SELECT RAISE(IGNORE); END",
	} {
		t.Run(trigger, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			fixture.sql(t, trigger)
			if _, err := fixture.service.claim(t.Context(), "worker"); err == nil {
				t.Fatal("claim ignored a missing durable write")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='pending' AND attempt_count=0 AND lease_token_hash IS NULL", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_delivery_attempts", 0)
		})
	}
}

func TestCompletionRollsBackEveryDurableBoundary(t *testing.T) {
	cases := []struct {
		statement string
		result    model.NotificationSendResult
	}{
		{"CREATE TRIGGER fail_delivery BEFORE UPDATE ON notification_deliveries BEGIN SELECT RAISE(IGNORE); END", model.NotificationSendResult{Success: true}},
		{"CREATE TRIGGER fail_attempt BEFORE UPDATE ON notification_delivery_attempts BEGIN SELECT RAISE(ABORT,'injected'); END", model.NotificationSendResult{Success: true}},
		{"CREATE TRIGGER fail_health BEFORE UPDATE ON notification_channel_health BEGIN SELECT RAISE(ABORT,'injected'); END", model.NotificationSendResult{Success: true}},
		{"CREATE TRIGGER fail_failure_message BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END", model.NotificationSendResult{ErrorCode: "http_rejected"}},
	}
	for _, test := range cases {
		t.Run(test.statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			lease := fixture.lease(t)
			fixture.sql(t, test.statement)
			if err := fixture.service.complete(t.Context(), completion{lease: lease, result: test.result}); err == nil {
				t.Fatal("completion ignored a durable write failure")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_delivery_attempts WHERE outcome='in_progress'", 1)
		})
	}
}
