//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

func TestLeaseRejectsChangedCapabilityAndPolicy(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	digest := advertise(t, f)
	job := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)

	// 目标在排队后被策略收紧：租约必须拒绝，而不是派发过期授权。
	saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) {
		policy.Enabled = true
		policy.AllowedKinds = []string{model.DiagnosticKindIperf3}
	})
	if _, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tightened policy lease error = %v", err)
	}
	enableMTRPolicy(t, f, false)

	// 节点改成只上报 iperf3 能力后，排队中的 MTR 作业不再可派发。
	iperfDigest := advertiseIperf3(t, f)
	if _, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: iperfDigest}); !errors.Is(err, ErrCapability) {
		t.Fatalf("capability mismatch lease error = %v", err)
	}
	_ = job
}

func TestLeaseRejectsUnresolvableFamily(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	auto, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: model.DiagnosticTargetInput{
		Name: "auto-family", Kinds: []string{model.DiagnosticKindMTR}, Address: "93.184.216.34",
		IPFamily: model.IPFamilyAuto, Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
		MaximumBitrateMbps: 100, MaximumDuration: 10, MaximumParallelStream: 1, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	digest := advertise(t, f)
	job, err := f.service.CreateJob(context.Background(), f.owner, JobInput{SourceNodeID: f.nodeID, TargetID: auto.ID,
		Kind: model.DiagnosticKindMTR, Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
		IPFamily: model.IPFamilyIPv6, HopLimit: 32})
	if err != nil {
		t.Fatal(err)
	}
	if job.IPFamily != model.IPFamilyIPv6 {
		t.Fatalf("job family = %s", job.IPFamily)
	}
	if _, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest}); !errors.Is(err, ErrCapability) {
		t.Fatalf("unresolvable family lease error = %v", err)
	}
}

func TestAttemptAuthenticationBranches(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	digest := advertise(t, f)
	job := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)
	lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: digest})
	if err != nil || lease == nil {
		t.Fatalf("lease = %+v / %v", lease, err)
	}
	invalidToken := f.token[:len(f.token)-2] + "zz"
	cases := map[string]AttemptRequest{
		"unknown job":  {Proof: NodeProof{Token: f.token}, JobID: "missing", Attempt: lease.Attempt, Nonce: lease.Nonce},
		"zero attempt": {Proof: NodeProof{Token: f.token}, JobID: job.ID, Attempt: 0, Nonce: lease.Nonce},
		"wrong nonce":  {Proof: NodeProof{Token: f.token}, JobID: job.ID, Attempt: lease.Attempt, Nonce: "00"},
		"bad token":    {Proof: NodeProof{Token: invalidToken}, JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if err := f.service.Start(context.Background(), request); err == nil {
				t.Fatalf("start must fail for %s", name)
			}
			if _, err := f.service.Heartbeat(context.Background(), request); err == nil {
				t.Fatalf("heartbeat must fail for %s", name)
			}
		})
	}
	if err := f.service.Start(context.Background(), AttemptRequest{Proof: NodeProof{Token: f.token},
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce}); err != nil {
		t.Fatal(err)
	}
	completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatal(err)
	}
	secondTarget := createMTRTarget(t, f, "coverage-target-2", "93.184.216.34", false)
	expired := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), secondTarget)
	expiredLease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest})
	if err != nil || expiredLease == nil {
		t.Fatalf("expired lease = %+v / %v", expiredLease, err)
	}
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_attempts SET leased_at=? WHERE job_id=?`,
		f.now.Add(-10*leaseDuration), expired.ID); err != nil {
		t.Fatal(err)
	}
	expiredAttempt := AttemptRequest{Proof: NodeProof{Token: f.token}, JobID: expired.ID,
		Attempt: expiredLease.Attempt, Nonce: expiredLease.Nonce}
	if err := f.service.Start(context.Background(), expiredAttempt); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired lease start error = %v", err)
	}
	if _, err := f.service.Heartbeat(context.Background(), expiredAttempt); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired lease heartbeat error = %v", err)
	}
}
