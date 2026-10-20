package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

func cleanupFaultRequest(t *testing.T, fixture discoveryFixture) EnrollmentRequest {
	t.Helper()
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approvalFor(request, "edge-cleanup"), fixture.now); err != nil {
		t.Fatal(err)
	}
	return request
}

func seedOrphanEnrollmentNode(t *testing.T, fixture discoveryFixture, id string) {
	t.Helper()
	if _, err := fixture.store.db.ExecContext(t.Context(), `INSERT INTO nodes(id, name, host, port,
		credential_state, agent_token_hash, lifecycle_state, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, NULL, 'active', ?, ?)`,
		id, id, "192.0.2.20", 22, string(agentenrollment.CredentialPendingClaim),
		fixture.now, fixture.now); err != nil {
		t.Fatal(err)
	}
}

// TestEnrollmentCleanupFaults 覆盖清理事务中票据、快照、保留节点、审计与提交的失败。
func TestEnrollmentCleanupFaults(t *testing.T) {
	later := func(fixture discoveryFixture) time.Time { return fixture.now.Add(4 * 24 * time.Hour) }
	t.Run("ticket prune blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_ticket
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'ticket blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("blocked ticket prune was accepted")
		}
	})
	t.Run("snapshot blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_snapshot
			BEFORE UPDATE ON agent_enrollment_requests WHEN NEW.status <> 'expired'
			BEGIN SELECT RAISE(ABORT,'snapshot blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("blocked node name snapshot was accepted")
		}
	})
	t.Run("expire blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_expire
			BEFORE UPDATE ON agent_enrollment_requests WHEN NEW.status = 'expired'
			BEGIN SELECT RAISE(ABORT,'expire blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("blocked request expiry was accepted")
		}
	})
	t.Run("reservation delete blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_reservation
			BEFORE DELETE ON nodes BEGIN SELECT RAISE(ABORT,'reservation blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("blocked reservation delete was accepted")
		}
	})
	t.Run("orphan delete blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		seedOrphanEnrollmentNode(t, fixture, "cov-orphan-node")
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_orphan
			BEFORE DELETE ON nodes BEGIN SELECT RAISE(ABORT,'orphan blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), fixture.now); err == nil {
			t.Fatal("blocked orphan delete was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("blocked cleanup audit was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		cleanupFaultRequest(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TABLE cov_cleanup_child(
			request TEXT REFERENCES agent_enrollment_requests(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_cleanup_commit
			AFTER UPDATE ON agent_enrollment_requests BEGIN INSERT INTO cov_cleanup_child VALUES('missing'); END`)
		if _, err := fixture.store.CleanupEnrollmentState(t.Context(), later(fixture)); err == nil {
			t.Fatal("deferred cleanup commit failure was accepted")
		}
	})
}
