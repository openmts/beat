//go:build linux

package diagnostics

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func mustPolicy(t *testing.T, f *fixture) model.DiagnosticPolicy {
	t.Helper()
	policy, err := f.service.Policy(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func enableMTRPolicy(t *testing.T, f *fixture, allowPrivate bool) model.DiagnosticPolicy {
	t.Helper()
	policy := mustPolicy(t, f)
	policy.Enabled = true
	policy.AllowedKinds = []string{model.DiagnosticKindMTR}
	policy.AllowPrivateTargets = allowPrivate
	saved, err := f.service.SavePolicy(context.Background(), f.owner, policy)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != policy.Revision+1 || !saved.Enabled {
		t.Fatalf("saved policy = %+v", saved)
	}
	return saved
}

func createMTRTarget(t *testing.T, f *fixture, name, address string, private bool) model.DiagnosticTarget {
	t.Helper()
	target, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: model.DiagnosticTargetInput{
		Name: name, Purpose: "fixture", Kinds: []string{model.DiagnosticKindMTR}, Address: address,
		Port: 0, IPFamily: model.IPFamilyIPv4, Protocol: model.DiagnosticProtocolICMP,
		Direction: model.DiagnosticDirectionForward, PrivateAddress: private,
		MaximumBitrateMbps: 100, MaximumDuration: 10, MaximumParallelStream: 1, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if target.Revision != 1 || len(target.ResolvedAddresses) != 1 {
		t.Fatalf("created target = %+v", target)
	}
	return target
}

func advertise(t *testing.T, f *fixture) string {
	t.Helper()
	digest, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, CapabilityDocument{Version: 1,
		Kinds: []string{model.DiagnosticKindMTR}, State: "ready",
		MTR:    &MTRCapability{Binary: "/usr/bin/mtr", Version: "0.95", IPFamilies: []string{model.IPFamilyIPv4}, Protocols: []string{model.DiagnosticProtocolICMP}},
		Maxima: CapabilityMaxima{Probes: 20, HopLimit: 32, DurationSeconds: 30, ParallelStreams: 1, BitrateMbps: 100}})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := f.service.Capabilities(context.Background(), f.owner)
	if err != nil || len(capabilities) != 1 || capabilities[0].State != "ready" || capabilities[0].NodeName != "Node A" {
		t.Fatalf("capabilities = %+v / %v", capabilities, err)
	}
	return digest
}

func createMTRJob(t *testing.T, f *fixture, policy model.DiagnosticPolicy, target model.DiagnosticTarget) model.DiagnosticJob {
	t.Helper()
	job, err := f.service.CreateJob(context.Background(), f.owner, JobInput{SourceNodeID: f.nodeID,
		TargetID: target.ID, Kind: model.DiagnosticKindMTR, Protocol: model.DiagnosticProtocolICMP,
		Direction: model.DiagnosticDirectionForward, IPFamily: model.IPFamilyIPv4, HopLimit: 32})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := f.service.Job(context.Background(), f.owner, job.ID)
	if err != nil || detail.Job.SourceNodeName != "Node A" || detail.Job.TargetName != target.Name {
		t.Fatalf("job detail = %+v / %v", detail, err)
	}
	_ = policy
	return job
}

func assertQueuedJob(t *testing.T, job model.DiagnosticJob) {
	t.Helper()
	if job.State != model.DiagnosticStateQueued || job.ReservedBytes != 0 || job.PolicyRevision == 0 {
		t.Fatalf("queued job = %+v", job)
	}
}

func leaseJob(t *testing.T, f *fixture, digest string) *Lease {
	t.Helper()
	lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: digest})
	if err != nil || lease == nil {
		t.Fatalf("lease = %+v / %v", lease, err)
	}
	if _, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: "wrong"}); err == nil {
		t.Fatal("stale capability digest must be rejected")
	}
	return lease
}

func progress(t *testing.T, f *fixture, jobID string, lease *Lease) {
	t.Helper()
	frame := ProgressFrame{Proof: NodeProof{Token: f.token}, ProgressRequest: diagnosticprotocol.ProgressRequest{
		JobID: jobID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1,
		Samples: []diagnosticprotocol.Sample{{Metric: model.DiagnosticMetricMTRLatencyMS, Hop: 1,
			Stat: model.DiagnosticStatAvg, Value: 12.5}},
		Hops: []HopIdentity{{Index: 1, Address: "93.184.216.34", Reached: true}}}}
	acknowledged, err := f.service.Progress(context.Background(), frame)
	if err != nil || acknowledged != 1 {
		t.Fatalf("progress = %d / %v", acknowledged, err)
	}
	if ack, err := f.service.Progress(context.Background(), frame); err != nil || ack != 1 || len(f.metrics.batches) != 1 {
		t.Fatalf("duplicate progress = %d / %v / %d", ack, err, len(f.metrics.batches))
	}
	detail, err := f.service.Job(context.Background(), f.owner, jobID)
	if err != nil || len(detail.Hops) != 1 || detail.Hops[0].Address != "93.184.216.34" {
		t.Fatalf("hops = %+v / %v", detail.Hops, err)
	}
}

func completeJob(t *testing.T, f *fixture, jobID string, lease *Lease) {
	t.Helper()
	completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
		JobID: jobID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatalf("idempotent completion failed: %v", err)
	}
	job, err := f.service.Job(context.Background(), f.owner, jobID)
	if err != nil || job.Job.State != model.DiagnosticStateSucceeded || job.Job.CompletedAt == nil {
		t.Fatalf("completed job = %+v / %v", job.Job, err)
	}
}

func assertCompletedResults(t *testing.T, f *fixture, jobID string) {
	t.Helper()
	f.metrics.points = []store.TimePoint{{Timestamp: f.now, Value: 17.5}}
	series, err := f.service.Results(context.Background(), f.owner, ResultQuery{JobID: jobID,
		Metric: model.DiagnosticMetricMTRLatencyMS, Hop: 1, Stat: model.DiagnosticStatAvg})
	if err != nil || len(series.Points) != 1 || series.Unit != "milliseconds" {
		t.Fatalf("results = %+v / %v", series, err)
	}
	if _, err := f.service.Results(context.Background(), f.owner, ResultQuery{JobID: jobID,
		Metric: model.DiagnosticMetricIperfBytesPerSecond}); err == nil {
		t.Fatal("mismatched metric must be rejected")
	}
}
