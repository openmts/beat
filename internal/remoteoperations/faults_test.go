package remoteoperations

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestDatabaseWriteFailuresRollback(t *testing.T) {
	for _, item := range []struct{ table, event, action string }{
		{"operation_policies", "INSERT", "policy"}, {"operation_policy_nodes", "DELETE", "policy"}, {"operation_policy_groups", "DELETE", "policy"},
		{"operation_policy_nodes", "INSERT", "policy"}, {"operation_tasks", "INSERT", "create"}, {"operation_task_targets", "INSERT", "create"},
		{"operation_attempts", "INSERT", "lease"}, {"operation_task_targets", "UPDATE", "lease"},
		{"operation_attempts", "UPDATE", "start"}, {"operation_tasks", "UPDATE", "cancel"},
		{"operation_attempts", "UPDATE", "cancel"}, {"operation_task_targets", "UPDATE", "cancel"},
		{"operation_attempts", "UPDATE", "complete"}, {"operation_task_targets", "UPDATE", "complete"},
	} {
		t.Run(item.table+item.event+item.action, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "seed")
			request := f.lease(t)
			if err := f.service.Start(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if item.table == "operation_policy_groups" {
				f.sql(t, "INSERT INTO operation_policy_groups(policy_id,group_id) SELECT ?,group_id FROM nodes WHERE id=?", f.policyID, f.nodeID)
			}
			f.sql(t, "CREATE TRIGGER deny_operation BEFORE "+item.event+" ON "+item.table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			var err error
			switch item.action {
			case "policy":
				_, err = f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Update", Policy: f.policy, NodeIDs: []string{f.nodeID}})
			case "create":
				_, err = f.service.CreateTask(t.Context(), f.actor, f.input("second"))
			case "lease":
				f.sql(t, "DROP TRIGGER deny_operation")
				if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}}); err != nil {
					t.Fatal(err)
				}
				f.task(t, "second")
				f.sql(t, "CREATE TRIGGER deny_operation BEFORE "+item.event+" ON "+item.table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
				_, err = f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest})
			case "start":
				err = f.service.Start(t.Context(), request)
			case "cancel":
				err = f.service.CancelTask(t.Context(), f.actor, id)
			case "complete":
				err = f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}})
			}
			if err == nil {
				t.Fatal("injected write failure ignored")
			}
		})
	}
}

func TestDatabaseReadFailuresFailClosed(t *testing.T) {
	for _, table := range []string{"admin_sessions", "operation_tasks", "operation_policies", "operation_capabilities", "operation_policy_nodes", "operation_task_targets"} {
		t.Run(table, func(t *testing.T) {
			f := testFixture(t)
			f.sql(t, "ALTER TABLE "+table+" RENAME TO fixture_unavailable")
			if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("failure")); err == nil {
				t.Fatal("missing dependency accepted")
			}
		})
	}
}

type brokenSecrets struct {
	base       secretbox.ResourceService
	seal, open bool
}

type brokenAccess struct {
	base       secretbox.ResourceAccess
	seal, open bool
}

func (broken brokenSecrets) WithWrite(ctx context.Context, write func(secretbox.ResourceAccess) error) error {
	return broken.base.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		return write(brokenAccess{access, broken.seal, broken.open})
	})
}

func (broken brokenSecrets) OpenResource(ctx context.Context, value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	return broken.base.OpenResource(ctx, value, aad)
}

func (broken brokenAccess) Seal(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if broken.seal {
		return nil, errors.New("injected seal failure")
	}
	return broken.base.Seal(value, aad)
}

func (broken brokenAccess) Open(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if broken.open {
		return nil, errors.New("injected open failure")
	}
	return broken.base.Open(value, aad)
}

func TestCryptoFailuresAndUnsupportedFingerprint(t *testing.T) {
	for _, mode := range []string{"seal", "open", "fingerprint"} {
		t.Run(mode, func(t *testing.T) {
			f := testFixture(t)
			f.task(t, "existing")
			f.service.secrets = brokenSecrets{base: f.service.secrets, seal: mode == "seal", open: mode == "open"}
			key := "other"
			if mode == "open" {
				key = "existing"
			}
			if _, err := f.service.CreateTask(t.Context(), f.actor, f.input(key)); err != ErrUnavailable {
				t.Fatal(err)
			}
		})
	}
}
