package notificationqueue

import (
	"testing"
	"time"
)

func TestPruneRollsBackEachMaintenanceBoundary(t *testing.T) {
	for _, statement := range []string{
		"CREATE TRIGGER fail_prune BEFORE DELETE ON notification_deliveries BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER fail_prune BEFORE DELETE ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER fail_prune BEFORE DELETE ON notification_request_budgets BEGIN SELECT RAISE(ABORT,'injected'); END",
		"ALTER TABLE alert_channel_revisions RENAME COLUMN revision TO missing_revision",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			fixture.sql(t, "UPDATE notification_deliveries SET state='succeeded',completed_at=1", "INSERT INTO notification_request_budgets VALUES('preview','actor',1,1)", statement)
			if err := fixture.service.Prune(t.Context()); err == nil {
				t.Fatal("maintenance ignored storage failure")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_messages", 1)
		})
	}
}

func TestReconciliationRollsBackOnMessageOrAuditFailure(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	fixture.sql(t, "UPDATE notification_deliveries SET state='dead',completed_at=1,error_code='lease_expired'", "CREATE TRIGGER fail_reconcile BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err := fixture.service.maintain(t.Context()); err == nil {
		t.Fatal("missing failure message was discarded")
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_messages", 1)
	fixture.sql(t, "DROP TRIGGER fail_reconcile", "ALTER TABLE notification_messages RENAME COLUMN event_kind TO missing_kind")
	if err := fixture.service.ReconcileFailures(t.Context()); err == nil {
		t.Fatal("reconciliation ignored unavailable source metadata")
	}
}

func TestLeaseRecoveryRollsBackAttemptAndTerminalWriteFailures(t *testing.T) {
	for _, statement := range []string{
		"CREATE TRIGGER fail_expiry BEFORE UPDATE ON notification_delivery_attempts BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER fail_expiry BEFORE UPDATE ON notification_deliveries BEGIN SELECT RAISE(IGNORE); END",
		"CREATE TRIGGER fail_expiry BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			fixture.sql(t, "UPDATE notification_deliveries SET attempt_count=5")
			_ = fixture.lease(t)
			now := fixture.service.now().Add(deliveryLeaseLifetime + time.Second)
			fixture.service.now = func() time.Time { return now }
			fixture.sql(t, statement)
			if _, err := fixture.service.claim(t.Context(), "recovery"); err == nil {
				t.Fatal("lease recovery ignored a persistence failure")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_delivery_attempts WHERE outcome='in_progress'", 1)
		})
	}
}
