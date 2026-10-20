//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDiagnosticJobLifecyclePersistsStateAndSamples(t *testing.T) {
	f := newFixture(t)
	policy := enableMTRPolicy(t, f, true)
	target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
	digest := advertise(t, f)
	job := createMTRJob(t, f, policy, target)
	assertQueuedJob(t, job)

	lease := leaseJob(t, f, digest)
	if lease.Payload.Destination != "93.184.216.34" || lease.Payload.Probes != model.DefaultDiagnosticProbes {
		t.Fatalf("lease payload = %+v", lease.Payload)
	}
	attempt := AttemptRequest{Proof: NodeProof{Token: f.token}, JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce}
	if err := f.service.Start(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Start(context.Background(), attempt); err != nil {
		t.Fatal("idempotent start must succeed")
	}
	progress(t, f, job.ID, lease)
	cancel, err := f.service.Heartbeat(context.Background(), attempt)
	if err != nil || cancel {
		t.Fatalf("heartbeat = %v / %v", cancel, err)
	}
	completeJob(t, f, job.ID, lease)
	assertCompletedResults(t, f, job.ID)
}

func TestDiagnosticServiceDeniesForbiddenDestinationsAndActors(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, true)
	_, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: model.DiagnosticTargetInput{
		Name: "loopback", Kinds: []string{model.DiagnosticKindMTR}, Address: "127.0.0.1", Protocol: model.DiagnosticProtocolICMP,
		Direction: model.DiagnosticDirectionForward, IPFamily: model.IPFamilyIPv4, MaximumBitrateMbps: 100,
		MaximumDuration: 10, MaximumParallelStream: 1, Enabled: true}})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("loopback target error = %v", err)
	}
	policy := mustPolicy(t, f)
	if _, err := f.service.SavePolicy(context.Background(), Actor{UserID: "admin", Role: "admin", SessionID: "session-admin"},
		policy); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin policy write error = %v", err)
	}
}

func TestDiagnosticJobCancelAndRetryFlow(t *testing.T) {
	f := newFixture(t)
	policy := enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
	advertise(t, f)
	job := createMTRJob(t, f, policy, target)
	if err := f.service.CancelJob(context.Background(), f.owner, job.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.Job(context.Background(), f.owner, job.ID)
	if err != nil || cancelled.Job.State != model.DiagnosticStateCancelled {
		t.Fatalf("cancelled job = %+v / %v", cancelled.Job.State, err)
	}
	if err := f.service.CancelJob(context.Background(), f.owner, job.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancel twice error = %v", err)
	}
	retried, err := f.service.RetryJob(context.Background(), f.owner, job.ID)
	if err != nil || retried.State != model.DiagnosticStateQueued {
		t.Fatalf("retry = %+v / %v", retried.State, err)
	}
	if err := f.service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteJob(context.Background(), f.owner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing job error = %v", err)
	}
}

func TestDiagnosticPolicyDefaultsToDisabled(t *testing.T) {
	f := newFixture(t)
	policy, err := f.service.Policy(context.Background(), f.owner)
	if err != nil || policy.Enabled || policy.Revision != 1 {
		t.Fatalf("default policy = %+v / %v", policy, err)
	}
	if allowsKind(policy, model.DiagnosticKindIperf3) {
		t.Fatal("default policy must not allow iperf3")
	}
}
