package remoteoperations

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestTaskLifecycleIsDurableIdempotentAndEncrypted(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "request")
	duplicate, err := f.service.CreateTask(t.Context(), f.actor, f.input("request"))
	if err != nil || duplicate != id {
		t.Fatalf("duplicate: %v", err)
	}
	changed := f.input("request")
	changed.Command = []byte("different")
	if _, err := f.service.CreateTask(t.Context(), f.actor, changed); err != ErrConflict {
		t.Fatal(err)
	}
	request := f.lease(t)
	result := Completion{Attempt: request, Result: Result{State: "succeeded", Stdout: []byte("private-output")}}
	if err := f.service.Complete(t.Context(), result); err != ErrConflict {
		t.Fatal("completion before start", err)
	}
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal("start retry", err)
	}
	if cancel, err := f.service.Heartbeat(t.Context(), request); err != nil || cancel {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), result); err != nil {
		t.Fatal("result retry", err)
	}
	result.Result.Stdout = []byte("conflict")
	if err := f.service.Complete(t.Context(), result); err != ErrConflict {
		t.Fatal(err)
	}
	var payload, output []byte
	var messages int
	if err := f.database.DB.QueryRow("SELECT payload_encrypted FROM operation_tasks WHERE id=?", id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB.QueryRow("SELECT stdout_encrypted FROM operation_attempts WHERE id=?", request.ID).Scan(&output); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("private-output")) || bytes.Contains(output, []byte("private-output")) {
		t.Fatal("plaintext persisted")
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE source_kind='operation'").Scan(&messages); err != nil || messages != 1 {
		t.Fatalf("notification count %d: %v", messages, err)
	}
}

func TestCancelRemainsAvailableWithoutRecentAuthentication(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "cancel")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE admin_sessions SET reauthenticated_until=NULL")
	if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("other")); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.CancelTask(t.Context(), f.actor, id); err != nil {
		t.Fatal(err)
	}
	if cancel, err := f.service.Heartbeat(t.Context(), request); err != nil || !cancel {
		t.Fatal(cancel, err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "canceled"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditAndOutboxFailuresRollbackOperationMutation(t *testing.T) {
	f := testFixture(t)
	f.sql(t, "CREATE TRIGGER deny_operation_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'fixture'); END")
	if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("audit")); err != ErrUnavailable {
		t.Fatal(err)
	}
	f.sql(t, "DROP TRIGGER deny_operation_audit")
	f.task(t, "audit")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	f.sql(t, "CREATE TRIGGER deny_operation_outbox BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'fixture'); END")
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}}); err == nil {
		t.Fatal("outbox failure accepted")
	}
	var state string
	if err := f.database.DB.QueryRow("SELECT state FROM operation_attempts WHERE id=?", request.ID).Scan(&state); err != nil || state != "running" {
		t.Fatal(state, err)
	}
}

func TestLeaseExpiryNeverRequeuesStartedAttempts(t *testing.T) {
	f := testFixture(t)
	f.task(t, "expiry")
	expired := f.lease(t)
	now := f.service.now().Add(LeaseDuration + time.Second)
	f.service.now = func() time.Time { return now }
	if err := f.service.Start(t.Context(), expired); err != ErrConflict {
		t.Fatal(err)
	}
	replacement := f.lease(t)
	if replacement.ID == expired.ID {
		t.Fatal("attempt id reused")
	}
	if err := f.service.Start(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	now = now.Add(HeartbeatGrace + time.Second)
	if lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != nil || lease != nil {
		t.Fatal(lease, err)
	}
	var state string
	if err := f.database.DB.QueryRow("SELECT state FROM operation_attempts WHERE id=?", replacement.ID).Scan(&state); err != nil || state != "unknown" {
		t.Fatal(state, err)
	}
}

func TestConcurrentLeaseAdmitsOneAttempt(t *testing.T) {
	f := testFixture(t)
	f.task(t, "concurrent")
	var wg sync.WaitGroup
	results := make(chan *Lease, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest})
			results <- lease
			errors <- err
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	count := 0
	for lease := range results {
		if lease != nil {
			count++
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("expected one lease, got %d", count)
	}
}
