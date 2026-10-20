//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func saveCoveragePolicy(t *testing.T, f *fixture, mutate func(*model.DiagnosticPolicy)) model.DiagnosticPolicy {
	t.Helper()
	policy := mustPolicy(t, f)
	mutate(&policy)
	saved, err := f.service.SavePolicy(context.Background(), f.owner, policy)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func enableCoveragePolicy(t *testing.T, f *fixture, kinds ...string) model.DiagnosticPolicy {
	t.Helper()
	return saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) {
		policy.Enabled = true
		policy.AllowedKinds = kinds
	})
}

func advertiseIperf3(t *testing.T, f *fixture) string {
	t.Helper()
	digest, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, CapabilityDocument{
		Version: 1, Kinds: []string{model.DiagnosticKindIperf3}, State: "ready",
		Iperf3: &Iperf3Capability{Binary: "/usr/bin/iperf3", Version: "3.17.1",
			Protocols: []string{model.DiagnosticProtocolTCP}, Directions: []string{model.DiagnosticDirectionForward}},
		Maxima: CapabilityMaxima{Probes: 20, HopLimit: 32, DurationSeconds: 30, ParallelStreams: 4, BitrateMbps: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func createCoverageIperfTarget(t *testing.T, f *fixture) model.DiagnosticTarget {
	t.Helper()
	target, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: model.DiagnosticTargetInput{
		Name: "coverage-iperf", Kinds: []string{model.DiagnosticKindIperf3}, Address: "93.184.216.34", Port: 5201,
		IPFamily: model.IPFamilyIPv4, Protocol: model.DiagnosticProtocolTCP, Direction: model.DiagnosticDirectionForward,
		MaximumBitrateMbps: 500, MaximumDuration: 20, MaximumParallelStream: 2, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func iperfJobInput(target model.DiagnosticTarget) JobInput {
	return JobInput{SourceNodeID: "node-a", TargetID: target.ID, Kind: model.DiagnosticKindIperf3,
		Protocol: model.DiagnosticProtocolTCP, Direction: model.DiagnosticDirectionForward,
		IPFamily: model.IPFamilyIPv4, DurationSeconds: 10, ParallelStreams: 2, BitrateMbps: 50,
		Confirmation: DiagnosticIperfConfirmation}
}

func TestCreateJobValidatesInput(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	advertise(t, f)
	cases := []struct {
		name   string
		mutate func(*JobInput)
	}{
		{"unknown kind", func(in *JobInput) { in.Kind = "dns" }},
		{"empty source", func(in *JobInput) { in.SourceNodeID = "" }},
		{"empty target", func(in *JobInput) { in.TargetID = "" }},
		{"probes too low", func(in *JobInput) {
			in.Probes = 0
			in.Probes = 0
			in.Kind = model.DiagnosticKindMTR
			in.HopLimit = 0
			in.Probes = model.MinimumDiagnosticProbes - 1
		}},
		{"hop limit too high", func(in *JobInput) { in.HopLimit = model.MaximumDiagnosticHops + 1 }},
		{"iperf duration", func(in *JobInput) {
			in.Kind = model.DiagnosticKindIperf3
			in.DurationSeconds = model.HardMaximumDiagnosticDurationSeconds + 1
		}},
		{"iperf streams", func(in *JobInput) {
			in.Kind = model.DiagnosticKindIperf3
			in.ParallelStreams = model.MaximumDiagnosticStreams + 1
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := JobInput{SourceNodeID: f.nodeID, TargetID: target.ID, Kind: model.DiagnosticKindMTR,
				Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
				IPFamily: model.IPFamilyIPv4, HopLimit: 32}
			test.mutate(&input)
			if _, err := f.service.CreateJob(context.Background(), f.owner, input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want invalid", err)
			}
		})
	}
}

func TestCreateJobPolicyAndTargetGates(t *testing.T) {
	f := newFixture(t)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	advertise(t, f)
	base := JobInput{SourceNodeID: f.nodeID, TargetID: target.ID, Kind: model.DiagnosticKindMTR,
		Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
		IPFamily: model.IPFamilyIPv4, HopLimit: 32}

	if _, err := f.service.CreateJob(context.Background(), f.owner, base); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled policy error = %v", err)
	}
	enableMTRPolicy(t, f, false)
	unknown := base
	unknown.TargetID = "missing"
	if _, err := f.service.CreateJob(context.Background(), f.owner, unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown target error = %v", err)
	}
	mismatch := base
	mismatch.Protocol = model.DiagnosticProtocolTCP
	if _, err := f.service.CreateJob(context.Background(), f.owner, mismatch); !errors.Is(err, ErrForbidden) {
		t.Fatalf("protocol mismatch error = %v", err)
	}
	family := base
	family.IPFamily = model.IPFamilyIPv6
	if _, err := f.service.CreateJob(context.Background(), f.owner, family); !errors.Is(err, ErrForbidden) {
		t.Fatalf("family mismatch error = %v", err)
	}
	if err := f.database.DB.QueryRowContext(context.Background(),
		`UPDATE diagnostic_targets SET enabled=0 WHERE id=?`, target.ID).Scan(); err == nil {
		t.Fatal("expected scan error for non-listing statement")
	}
	if _, err := f.service.CreateJob(context.Background(), f.owner, base); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled target error = %v", err)
	}
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_targets SET enabled=1, expires_at=? WHERE id=?`,
		f.now.Add(-time.Hour), target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateJob(context.Background(), f.owner, base); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired target error = %v", err)
	}
}

func TestCreateJobRequiresCapabilityAndConfirmation(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	base := JobInput{SourceNodeID: f.nodeID, TargetID: target.ID, Kind: model.DiagnosticKindMTR,
		Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
		IPFamily: model.IPFamilyIPv4, HopLimit: 32}
	if _, err := f.service.CreateJob(context.Background(), f.owner, base); !errors.Is(err, ErrCapability) {
		t.Fatalf("missing capability error = %v", err)
	}
	advertise(t, f)
	excessive := base
	excessive.HopLimit = 64
	if _, err := f.service.CreateJob(context.Background(), f.owner, excessive); !errors.Is(err, ErrCapability) {
		t.Fatalf("maxima error = %v", err)
	}

	enableCoveragePolicy(t, f, model.DiagnosticKindMTR, model.DiagnosticKindIperf3)
	iperfTarget := createCoverageIperfTarget(t, f)
	digest := advertiseIperf3(t, f)
	_ = digest
	withoutConfirmation := iperfJobInput(iperfTarget)
	withoutConfirmation.Confirmation = ""
	if _, err := f.service.CreateJob(context.Background(), f.owner, withoutConfirmation); !errors.Is(err, ErrInvalid) {
		t.Fatalf("confirmation error = %v", err)
	}
	job, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(iperfTarget))
	if err != nil {
		t.Fatal(err)
	}
	if job.ReservedBytes <= 0 || job.DurationSeconds != 10 {
		t.Fatalf("iperf job = %+v", job)
	}
	if _, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(iperfTarget)); !errors.Is(err, ErrConflict) {
		t.Fatalf("target concurrency error = %v", err)
	}
}

func TestListJobsFiltersAndRetryGuards(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	advertise(t, f)
	job := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)

	jobs, err := f.service.ListJobs(context.Background(), f.owner, JobFilter{State: model.DiagnosticStateQueued,
		SourceNode: f.nodeID, TargetID: target.ID, Limit: 1})
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("filtered jobs = %+v / %v", jobs, err)
	}
	if jobs, err := f.service.ListJobs(context.Background(), f.owner, JobFilter{State: "failed", Limit: 5000}); err != nil || len(jobs) != 0 {
		t.Fatalf("empty filter = %+v / %v", jobs, err)
	}
	if _, err := f.service.Job(context.Background(), f.owner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing job error = %v", err)
	}
	if _, err := f.service.RetryJob(context.Background(), f.owner, job.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry queued error = %v", err)
	}
	if err := f.service.CancelJob(context.Background(), insertCoverageSession(t, f, "session-coverage-admin", "admin", true), job.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign cancel error = %v", err)
	}
	if err := f.service.CancelJob(context.Background(), f.owner, job.ID); err != nil {
		t.Fatal(err)
	}
	admin := insertCoverageSession(t, f, "session-coverage-admin-2", "admin", true)
	if _, err := f.service.RetryJob(context.Background(), admin, job.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin retry error = %v", err)
	}
	retried, err := f.service.RetryJob(context.Background(), f.owner, job.ID)
	if err != nil || retried.State != model.DiagnosticStateQueued {
		t.Fatalf("retry = %+v / %v", retried, err)
	}
}
