package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/model"
)

func approvalFor(request EnrollmentRequest, name string) EnrollmentApproval {
	return EnrollmentApproval{RequestID: request.ID, NodeName: name, GroupID: "",
		Host: "", SSHPort: 22, ActorID: "owner-1"}
}

func TestApproveEnrollmentRequestValidation(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
		EnrollmentApproval{RequestID: "missing-request", NodeName: "edge-01", SSHPort: 22},
		fixture.now); !errors.Is(err, ErrEnrollmentRequestNotFound) {
		t.Fatalf("unknown request error = %v", err)
	}
	cases := map[string]EnrollmentApproval{
		"blank name":    approvalFor(request, "   "),
		"long name":     approvalFor(request, strings.Repeat("n", maxEnrollmentNodeNameLength+1)),
		"control char":  approvalFor(request, "edge\x0201"),
		"invalid tag":   {RequestID: request.ID, NodeName: "edge-01", Tags: []string{"a,b"}, SSHPort: 22},
		"ssh port zero": {RequestID: request.ID, NodeName: "edge-01", SSHPort: 0},
		"too many tags": {RequestID: request.ID, NodeName: "edge-01", Tags: make([]string, model.MaxNodeTags+1), SSHPort: 22},
	}
	for name, approval := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approval, fixture.now); err == nil {
				t.Fatal("invalid approval was accepted")
			}
		})
	}
	if _, err := fixture.nodes.UpsertNode(t.Context(), "taken-name", "10.0.0.9", 22); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
		approvalFor(request, "taken-name"), fixture.now); !errors.Is(err, ErrNodeNameConflict) {
		t.Fatalf("taken node name error = %v", err)
	}
	if _, err := fixture.store.db.ExecContext(t.Context(),
		"UPDATE agent_enrollment_grants SET consumed_enrollments = maximum_enrollments WHERE id = ?",
		fixture.grant.ID); err != nil {
		t.Fatalf("exhaust capacity: %v", err)
	}
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
		approvalFor(request, "edge-01"), fixture.now); !errors.Is(err, ErrEnrollmentGrantCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
}

func TestApproveEnrollmentRequestDefaultsAndReplay(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	request, _ := fixture.createRequest(t, fixture.now)
	approved, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approvalFor(request, "edge-01"), fixture.now)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	var group, host string
	if err := fixture.store.db.QueryRowContext(t.Context(),
		"SELECT group_id, host FROM nodes WHERE id = ?", approved.ReservedNodeID.String).Scan(&group, &host); err != nil {
		t.Fatalf("read reserved node: %v", err)
	}
	if group != fixture.grant.DefaultGroupID || host != "10.0.0.5" {
		t.Fatalf("reserved node group=%s host=%s", group, host)
	}
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(),
		approvalFor(request, "edge-02"), fixture.now); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
		t.Fatalf("second approval error = %v", err)
	}
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: request.ID, ActorID: "owner-1", Reason: "changed my mind"}, fixture.now); err != nil {
		t.Fatalf("reject approved request: %v", err)
	}
	rejected, err := fixture.store.EnrollmentRequest(t.Context(), request.ID)
	if err != nil || rejected.Status != agentenrollment.RequestRejected || rejected.RejectionReason != "changed my mind" {
		t.Fatalf("rejected = %+v err = %v", rejected, err)
	}
	var hidden int
	if err := fixture.store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM nodes WHERE name = ?",
		"edge-01").Scan(&hidden); err != nil {
		t.Fatalf("count reserved node: %v", err)
	}
	if hidden != 0 {
		t.Fatalf("released reservation left %d nodes", hidden)
	}
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: request.ID, ActorID: "owner-1"}, fixture.now); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
		t.Fatalf("second rejection error = %v", err)
	}
}

func TestRejectEnrollmentRequestValidation(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: "missing-request", ActorID: "owner-1"}, fixture.now); !errors.Is(err, ErrEnrollmentRequestNotFound) {
		t.Fatalf("unknown request error = %v", err)
	}
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: request.ID, ActorID: "owner-1",
		Reason: strings.Repeat("r", maxRejectionReasonLength+1)}, fixture.now); !errors.Is(err, agentenrollment.ErrInvalidMetadata) {
		t.Fatalf("long reason error = %v", err)
	}
	rejected, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: request.ID, ActorID: "owner-1", Reason: "not ours"}, fixture.now)
	if err != nil || rejected.RejectedBy != "owner-1" || rejected.RejectedAt.Time.IsZero() {
		t.Fatalf("rejected = %+v err = %v", rejected, err)
	}
}

func TestClaimEnrollmentRequestGuards(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: "missing-request", Ticket: "ticket"}, token, fixture.now); !errors.Is(err, ErrEnrollmentRequestNotFound) {
		t.Fatalf("unknown request error = %v", err)
	}
	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate ticket: %v", err)
	}
	if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), "missing-request", ticket, fixture.now); !errors.Is(err, ErrEnrollmentTicketUnavailable) {
		t.Fatalf("rotate on missing request error = %v", err)
	}
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, fixture.now); !errors.Is(err, ErrEnrollmentTicketUnavailable) {
		t.Fatalf("rotate on pending request error = %v", err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, fixture.now); !errors.Is(err, ErrEnrollmentTicketUnavailable) {
		t.Fatalf("pending claim error = %v", err)
	}
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approvalFor(request, "edge-01"), fixture.now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	polled, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, fixture.now)
	if err != nil || polled.TicketGeneration != 1 {
		t.Fatalf("rotate ticket = %+v err = %v", polled, err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, fixture.now.Add(3*time.Minute)); !errors.Is(err, ErrEnrollmentTicketUnavailable) {
		t.Fatalf("expired ticket error = %v", err)
	}
	// 先让请求进入 claimed，再撤销授权：撤销不会改动已认领请求，因此能验证认领阶段的授权检查。
	claimed, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, fixture.now)
	if err != nil || claimed.Recovered {
		t.Fatalf("claim = %+v err = %v", claimed, err)
	}
	if _, err := fixture.store.RevokeGrant(t.Context(), fixture.grant.ID, "owner-1", fixture.now); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	revokedTicket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate revoked ticket: %v", err)
	}
	if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, revokedTicket, fixture.now); err != nil {
		t.Fatalf("rotate ticket after revoke: %v", err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: revokedTicket.Plaintext}, token, fixture.now); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
		t.Fatalf("revoked grant error = %v", err)
	}
}

func TestClaimEnrollmentRecoveryWindow(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	recoverable := grantPolicy(fixture.grant, "recoverable-fleet")
	recoverable.ClaimRecovery = true
	fixture.grant = mustCreateGrant(t, fixture, recoverable)
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), approvalFor(request, "edge-01"), fixture.now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	claim := func(at time.Time) (EnrollmentClaimResult, error) {
		t.Helper()
		ticket, err := agentenrollment.GenerateClaimTicket()
		if err != nil {
			t.Fatalf("generate ticket: %v", err)
		}
		if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, at); err != nil {
			t.Fatalf("rotate ticket: %v", err)
		}
		token, err := agentcredential.Generate()
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		return fixture.store.ClaimEnrollmentRequest(t.Context(),
			EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, at)
	}
	first, err := claim(fixture.now)
	if err != nil || first.Recovered {
		t.Fatalf("first claim = %+v err = %v", first, err)
	}
	recovered, err := claim(fixture.now.Add(time.Minute))
	if err != nil || !recovered.Recovered || recovered.NodeID != first.NodeID {
		t.Fatalf("recovery claim = %+v err = %v", recovered, err)
	}
	if _, err := fixture.store.db.ExecContext(t.Context(),
		"UPDATE agent_enrollment_requests SET recovery_deadline = ? WHERE id = ?",
		fixture.now.Add(-time.Minute), request.ID); err != nil {
		t.Fatalf("expire recovery window: %v", err)
	}
	if _, err := claim(fixture.now); !errors.Is(err, ErrEnrollmentRecoveryUnavailable) {
		t.Fatalf("expired recovery error = %v", err)
	}
	if _, err := fixture.store.db.ExecContext(t.Context(),
		"UPDATE agent_enrollment_requests SET recovery_deadline = ?, recovery_attempts = ?, reserved_node_id = NULL WHERE id = ?",
		fixture.now.Add(time.Hour), 0, request.ID); err != nil {
		t.Fatalf("clear reservation: %v", err)
	}
	if _, err := claim(fixture.now); !errors.Is(err, ErrEnrollmentRecoveryUnavailable) {
		t.Fatalf("missing reservation error = %v", err)
	}
}
