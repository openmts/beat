package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
)

// approveEnrollmentFixture 创建并审批一条请求，返回请求本体。
func approveEnrollmentFixture(t *testing.T, fixture discoveryFixture) EnrollmentRequest {
	t.Helper()
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approvalFor(request, "edge-01"), fixture.now); err != nil {
		t.Fatal(err)
	}
	return request
}

// rotateEnrollmentTicket 轮换票据并返回明文，供认领调用使用。
func rotateEnrollmentTicket(t *testing.T, fixture discoveryFixture, requestID string, at time.Time) string {
	t.Helper()
	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), requestID, ticket, at); err != nil {
		t.Fatal(err)
	}
	return ticket.Plaintext
}

// claimableEnrollment 返回一条已审批且票据就绪的认领输入与令牌。
func claimableEnrollment(t *testing.T, fixture discoveryFixture) (EnrollmentClaim, agentcredential.Token) {
	t.Helper()
	request := approveEnrollmentFixture(t, fixture)
	plaintext := rotateEnrollmentTicket(t, fixture, request.ID, fixture.now)
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return EnrollmentClaim{RequestID: request.ID, Ticket: plaintext}, token
}

// TestEnrollmentTicketRotationFaults 覆盖票据轮换的写入失败与回读失败。
func TestEnrollmentTicketRotationFaults(t *testing.T) {
	now := enrollmentFaultNow()
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		if _, err := closed.RotateEnrollmentTicket(t.Context(), "missing", agentenrollment.Secret{}, now); err == nil {
			t.Fatal("closed database rotated a ticket")
		}
	})
	t.Run("request unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request := approveEnrollmentFixture(t, fixture)
		installEnrollmentTrigger(t, fixture, "ALTER TABLE nodes RENAME TO cov_nodes_backup")
		ticket, err := agentenrollment.GenerateClaimTicket()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, fixture.now); err == nil {
			t.Fatal("unreadable request reread was accepted")
		}
	})
}

// TestEnrollmentClaimTransactionFaults 覆盖认领事务的授权查找、审计与提交失败。
func TestEnrollmentClaimTransactionFaults(t *testing.T) {
	now := enrollmentFaultNow()
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		token, err := agentcredential.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := closed.ClaimEnrollmentRequest(t.Context(),
			EnrollmentClaim{RequestID: "missing"}, token, now); err == nil {
			t.Fatal("closed database claimed a request")
		}
	})
	t.Run("grant lookup unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, "ALTER TABLE groups RENAME TO cov_groups_backup")
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("unreadable grant lookup was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_claim_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked claim audit was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TABLE cov_claim_child(
			node TEXT REFERENCES nodes(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_claim_commit AFTER UPDATE ON nodes
			BEGIN INSERT INTO cov_claim_child VALUES('missing-node'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("deferred claim commit failure was accepted")
		}
	})
	t.Run("pending status", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		ticket, err := agentenrollment.GenerateClaimTicket()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.db.ExecContext(t.Context(), `UPDATE agent_enrollment_requests
			SET ticket_hash = ?, ticket_expires_at = ? WHERE id = ?`,
			ticket.Hash, fixture.now.Add(2*time.Minute), request.ID); err != nil {
			t.Fatal(err)
		}
		token, err := agentcredential.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
			EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, fixture.now); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
			t.Fatalf("pending status error = %v", err)
		}
	})
}

// TestEnrollmentActivationFaults 覆盖首次认领的节点、令牌与状态写入失败。
func TestEnrollmentActivationFaults(t *testing.T) {
	t.Run("reservation detached", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, "UPDATE agent_enrollment_requests SET reserved_node_id = NULL")
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("request without reservation was claimed")
		}
	})
	t.Run("node row unscannable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture,
			"UPDATE nodes SET port = 'not-a-port' WHERE credential_state = 'pending_claim'")
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("unscannable node row was claimed")
		}
	})
	t.Run("token write blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_claim_node_token
			BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'token blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked node token write was accepted")
		}
	})
	t.Run("identity write blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_claim_identity
			BEFORE INSERT ON agent_enrollment_identities BEGIN SELECT RAISE(ABORT,'identity blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked identity write was accepted")
		}
	})
	t.Run("status update blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		claim, token := claimableEnrollment(t, fixture)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_claim_status
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'status blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked status update was accepted")
		}
	})
	t.Run("missing reservation", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		tx, err := beginWriteTx(t.Context(), fixture.store.db)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := reservedEnrollmentNode(t.Context(), tx, EnrollmentRequest{}); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
			t.Fatalf("missing reservation error = %v", err)
		}
	})
}

// recoverableEnrollment 让请求进入 claimed，并返回可用的恢复票据与令牌。
func recoverableEnrollment(t *testing.T) (discoveryFixture, EnrollmentClaim, agentcredential.Token) {
	t.Helper()
	fixture := newEnrollmentFixture(t)
	policy := grantPolicy(fixture.grant, "cov-recoverable")
	policy.ClaimRecovery = true
	fixture.grant = mustCreateGrant(t, fixture, policy)
	claim, token := claimableEnrollment(t, fixture)
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err != nil {
		t.Fatal(err)
	}
	plaintext := rotateEnrollmentTicket(t, fixture, claim.RequestID, fixture.now)
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return fixture, EnrollmentClaim{RequestID: claim.RequestID, Ticket: plaintext}, token
}

// TestEnrollmentRecoveryFaults 覆盖恢复认领的节点查找、令牌与状态写入失败。
func TestEnrollmentRecoveryFaults(t *testing.T) {
	t.Run("node not active", func(t *testing.T) {
		fixture, claim, token := recoverableEnrollment(t)
		installEnrollmentTrigger(t, fixture,
			"UPDATE nodes SET credential_state = 'pending_claim' WHERE credential_state = 'active'")
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("recovery without an active node was accepted")
		}
	})
	t.Run("token write blocked", func(t *testing.T) {
		fixture, claim, token := recoverableEnrollment(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_recover_token
			BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'token blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked recovery token write was accepted")
		}
	})
	t.Run("status update blocked", func(t *testing.T) {
		fixture, claim, token := recoverableEnrollment(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_recover_status
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'status blocked'); END`)
		if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(), claim, token, fixture.now); err == nil {
			t.Fatal("blocked recovery status update was accepted")
		}
	})
}
