package remoteoperations

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
)

func TestCurrentAuthorityAndDefaultDisabled(t *testing.T) {
	f := testFixture(t)
	if _, err := NewService(Config{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	service, err := NewService(Config{Database: f.database.DB, Secrets: f.service.secrets})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTask(t.Context(), f.actor, f.input("disabled")); err != ErrForbidden {
		t.Fatal(err)
	}
	if service.Complete(t.Context(), Completion{}) != ErrForbidden {
		t.Fatal("disabled completion")
	}
	if _, err := f.service.CreateTask(t.Context(), Actor{}, f.input("unauthorized")); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: "invalid"}, f.policy); err != ErrForbidden {
		t.Fatal(err)
	}
	unknown, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: unknown.Plaintext}, f.policy); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: f.token}, Policy{}); err != ErrInvalid {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE admin_users SET role='admin'")
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{Name: "Denied", Policy: f.policy}); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.CancelTask(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE admin_sessions SET revoked_at=?", f.service.now())
	if err := f.service.CancelTask(t.Context(), f.actor, "missing"); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestStartRechecksScopePolicyAndCapability(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE operation_policies SET enabled=0",
		"DELETE FROM operation_policy_nodes",
		"UPDATE operation_capabilities SET digest='changed'",
		"UPDATE operation_capabilities SET seen_at=0",
		"UPDATE nodes SET agent_token_revoked_at=CURRENT_TIMESTAMP",
	} {
		t.Run(mutation, func(t *testing.T) {
			f := testFixture(t)
			f.task(t, "admission")
			request := f.lease(t)
			f.sql(t, mutation)
			if err := f.service.Start(t.Context(), request); err == nil {
				t.Fatal("stale authority admitted")
			}
		})
	}
	f := testFixture(t)
	id := f.task(t, "canceled")
	request := f.lease(t)
	if err := f.service.CancelTask(t.Context(), f.actor, id); err != nil {
		t.Fatal(err)
	}
	if f.service.Start(t.Context(), request) != ErrConflict {
		t.Fatal("canceled started")
	}
	request.ID = "missing"
	if f.service.Start(t.Context(), request) != ErrNotFound {
		t.Fatal("unknown attempt")
	}
}

func TestScopeSelectionQuotaAndQueueExpiry(t *testing.T) {
	f := testFixture(t)
	bad := f.input("bad")
	bad.NodeIDs = append(bad.NodeIDs, "missing")
	if _, err := f.service.CreateTask(t.Context(), f.actor, bad); err != ErrInvalid {
		t.Fatal(err)
	}
	bad = f.input("bad")
	bad.Command = nil
	if _, err := f.service.CreateTask(t.Context(), f.actor, bad); err != ErrInvalid {
		t.Fatal(err)
	}
	bad = f.input("bad")
	bad.NodeIDs = nil
	if _, err := f.service.CreateTask(t.Context(), f.actor, bad); err != ErrInvalid {
		t.Fatal(err)
	}
	bad = f.input("bad")
	bad.PolicyID = "missing"
	if _, err := f.service.CreateTask(t.Context(), f.actor, bad); err != ErrNotFound {
		t.Fatal(err)
	}
	f.policy.MaximumActiveTasks = 1
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Quota", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatal(err)
	}
	f.task(t, "first")
	if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("second")); err != ErrForbidden {
		t.Fatal(err)
	}
	now := f.service.now().Add(QueueLifetime + time.Second)
	f.service.now = func() time.Time { return now }
	f.sql(t, "UPDATE operation_capabilities SET seen_at=?", now.UnixMilli())
	if lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != nil || lease != nil {
		t.Fatal(lease, err)
	}
}

func TestPolicyAssignmentsAndOptimisticVersion(t *testing.T) {
	f := testFixture(t)
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{}); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Name: "Stale", Policy: f.policy}); err != ErrConflict {
		t.Fatal(err)
	}
	var groupID string
	if err := f.database.DB.QueryRow("SELECT group_id FROM nodes WHERE id=?", f.nodeID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Group", Policy: f.policy, GroupIDs: []string{groupID}}); err != nil {
		t.Fatal(err)
	}
	input := f.input("groups")
	input.NodeIDs = nil
	input.GroupIDs = []string{groupID}
	if _, err := f.service.CreateTask(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE nodes SET group_id=NULL")
	if lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != nil || lease != nil {
		t.Fatal("out-of-scope work was dispatched", err)
	}
}

func TestLeaseAndResultBounds(t *testing.T) {
	f := testFixture(t)
	f.task(t, "bounds")
	if _, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: "stale"}); err != ErrConflict {
		t.Fatal(err)
	}
	request := f.lease(t)
	bad := request
	bad.Nonce = "bad"
	if err := f.service.Start(t.Context(), bad); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.Heartbeat(t.Context(), request); err != ErrConflict {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: bad, Result: Result{State: "failed"}}); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "failed", Stdout: make([]byte, 1025)}}); err != ErrInvalid {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "invalid"}}); err != ErrInvalid {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "failed"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "failed", ExitCode: 1}}); err != ErrConflict {
		t.Fatal(err)
	}
	if f.service.Start(t.Context(), request) != ErrConflict {
		t.Fatal("completed start")
	}
	if _, err := f.service.Heartbeat(t.Context(), request); err != ErrConflict {
		t.Fatal(err)
	}
}
