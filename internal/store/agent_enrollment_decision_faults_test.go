package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

func enrollmentFaultNow() time.Time {
	return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
}

func installEnrollmentTrigger(t *testing.T, fixture discoveryFixture, statement string) {
	t.Helper()
	if _, err := fixture.store.db.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

// TestEnrollmentDecisionTransactionFaults 覆盖审批/拒绝共用的读取、审计、提交与状态迁移失败。
func TestEnrollmentDecisionTransactionFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		now := enrollmentFaultNow()
		if _, err := closed.ApproveEnrollmentRequest(t.Context(),
			EnrollmentApproval{RequestID: "missing", NodeName: "edge-01", SSHPort: 22}, now); err == nil {
			t.Fatal("closed database approved a request")
		}
		if _, err := closed.RejectEnrollmentRequest(t.Context(),
			EnrollmentRejection{RequestID: "missing"}, now); err == nil {
			t.Fatal("closed database rejected a request")
		}
	})
	t.Run("grant lookup unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE groups RENAME TO cov_groups_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err == nil {
			t.Fatal("unreadable grant lookup was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_approve_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err == nil {
			t.Fatal("blocked approval audit was accepted")
		}
	})
	t.Run("decision reread missing", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, fmt.Sprintf(`CREATE TRIGGER cov_decision_vanished
			AFTER UPDATE ON agent_enrollment_requests
			BEGIN DELETE FROM agent_enrollment_requests WHERE id = '%s'; END`, request.ID))
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); !errors.Is(err, ErrEnrollmentRequestNotFound) {
			t.Fatalf("vanished decision = %v", err)
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installCustomMetricCommitFault(t, fixture.store.db, "INSERT ON nodes")
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err == nil {
			t.Fatal("deferred approval commit failure was accepted")
		}
	})
	t.Run("reread after commit", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, fmt.Sprintf(`CREATE TRIGGER cov_approve_reread
			AFTER INSERT ON admin_audit_events
			BEGIN DELETE FROM agent_enrollment_requests WHERE id = '%s'; END`, request.ID))
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); !errors.Is(err, ErrEnrollmentRequestNotFound) {
			t.Fatalf("reread after commit = %v", err)
		}
	})
	t.Run("reject update blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_reject_update
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'reject blocked'); END`)
		if _, err := fixture.store.RejectEnrollmentRequest(t.Context(),
			EnrollmentRejection{RequestID: request.ID, ActorID: "owner-1", Reason: "not ours"},
			fixture.now); err == nil {
			t.Fatal("blocked rejection was accepted")
		}
	})
	t.Run("capacity update blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_capacity_block
			BEFORE UPDATE ON agent_enrollment_grants BEGIN SELECT RAISE(ABORT,'capacity blocked'); END`)
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err == nil {
			t.Fatal("blocked capacity reservation was accepted")
		}
	})
	t.Run("approve update blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_approve_update
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'approve blocked'); END`)
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err == nil {
			t.Fatal("blocked approval update was accepted")
		}
	})
}

// TestEnrollmentRejectionReleaseFaults 覆盖拒绝时清理保留节点的三条语句失败。
func TestEnrollmentRejectionReleaseFaults(t *testing.T) {
	reject := func(t *testing.T, fixture discoveryFixture, request EnrollmentRequest) error {
		t.Helper()
		_, err := fixture.store.RejectEnrollmentRequest(t.Context(),
			EnrollmentRejection{RequestID: request.ID, ActorID: "owner-1", Reason: "superseded"}, fixture.now)
		return err
	}
	approved := func(t *testing.T) (discoveryFixture, EnrollmentRequest) {
		t.Helper()
		fixture := newEnrollmentFixture(t)
		request, _ := fixture.createRequest(t, fixture.now)
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			approvalFor(request, "edge-01"), fixture.now); err != nil {
			t.Fatal(err)
		}
		return fixture, request
	}
	t.Run("snapshot update blocked", func(t *testing.T) {
		fixture, request := approved(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_release_snapshot
			BEFORE UPDATE ON agent_enrollment_requests WHEN OLD.status = 'rejected'
			BEGIN SELECT RAISE(ABORT,'snapshot blocked'); END`)
		if err := reject(t, fixture, request); err == nil {
			t.Fatal("blocked snapshot refresh was accepted")
		}
	})
	t.Run("node delete blocked", func(t *testing.T) {
		fixture, request := approved(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_release_delete
			BEFORE DELETE ON nodes BEGIN SELECT RAISE(ABORT,'delete blocked'); END`)
		if err := reject(t, fixture, request); err == nil {
			t.Fatal("blocked reservation delete was accepted")
		}
	})
	t.Run("reservation clear blocked", func(t *testing.T) {
		fixture, request := approved(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_release_clear
			BEFORE UPDATE ON agent_enrollment_requests
			WHEN NEW.reserved_node_id IS NULL AND OLD.reserved_node_id IS NOT NULL
			BEGIN SELECT RAISE(ABORT,'clear blocked'); END`)
		if err := reject(t, fixture, request); err == nil {
			t.Fatal("blocked reservation clear was accepted")
		}
	})
}

// TestEnrollmentDecisionHelpersFailClosed 覆盖决策骨架内的辅助函数失败面。
func TestEnrollmentDecisionHelpersFailClosed(t *testing.T) {
	now := enrollmentFaultNow()
	t.Run("require single row", func(t *testing.T) {
		if err := requireSingleRow(nil, errors.New("driver failure"), "request-1"); err == nil {
			t.Fatal("driver failure was accepted")
		}
		broken := covResult{err: errors.New("rows affected unavailable")}
		if err := requireSingleRow(broken, nil, "request-1"); err == nil {
			t.Fatal("unreadable affected rows were accepted")
		}
		if err := requireSingleRow(covResult{affected: 0}, nil, "request-1"); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
			t.Fatalf("missing decision row = %v", err)
		}
	})
	t.Run("node name invalid utf8", func(t *testing.T) {
		if err := validateEnrollmentNodeName(string([]byte{0xff, 0xfe})); !errors.Is(err, agentenrollment.ErrInvalidMetadata) {
			t.Fatalf("invalid utf-8 name = %v", err)
		}
	})
	t.Run("request load unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE agent_enrollment_requests RENAME TO cov_requests_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
			EnrollmentApproval{RequestID: "any", NodeName: "edge-01", SSHPort: 22}, now); err == nil {
			t.Fatal("unreadable request table was accepted")
		}
	})
	t.Run("name check unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE nodes RENAME TO cov_nodes_backup"); err != nil {
			t.Fatal(err)
		}
		transaction := alertTestTx(t, fixture.store.db)
		if _, err := enrollmentNodeNameTaken(t.Context(), transaction, "edge-01"); err == nil {
			t.Fatal("unreadable node table was accepted")
		}
	})
	t.Run("derivation failure", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		request := EnrollmentRequest{Metadata: agentenrollment.Metadata{}}
		if _, _, ok := automaticEnrollmentReservation(fixture.grant, request); ok {
			t.Fatal("empty hostname derived a reservation")
		}
	})
	t.Run("auto approval on unreadable request", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		grant := mustCreateAutomaticGrant(t, fixture, nil)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE agent_enrollment_requests RENAME TO cov_requests_backup"); err != nil {
			t.Fatal(err)
		}
		transaction := alertTestTx(t, fixture.store.db)
		if err := autoApproveEnrollmentRequest(t.Context(), transaction, grant, "any", now); err == nil {
			t.Fatal("unreadable request table was accepted by automatic approval")
		}
	})
	t.Run("auto approval on decided request", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		grant := mustCreateAutomaticGrant(t, fixture, nil)
		request, _ := fixture.createRequest(t, fixture.now)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"UPDATE agent_enrollment_requests SET status = 'rejected' WHERE id = ?", request.ID); err != nil {
			t.Fatal(err)
		}
		transaction := alertTestTx(t, fixture.store.db)
		if err := autoApproveEnrollmentRequest(t.Context(), transaction, grant, request.ID, now); err != nil {
			t.Fatalf("decided request = %v", err)
		}
	})
	t.Run("conflict update unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE agent_enrollment_requests RENAME TO cov_requests_backup"); err != nil {
			t.Fatal(err)
		}
		transaction := alertTestTx(t, fixture.store.db)
		err := recordEnrollmentConflict(t.Context(), transaction,
			enrollmentConflict{id: "any", reason: enrollmentReasonPolicyConflict, now: now})
		if err == nil {
			t.Fatal("unreadable conflict update was accepted")
		}
	})
}
