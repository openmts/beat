//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

// failJob 把已租约的作业推进到失败终态，作为可重试与可删除的起点。
func failJob(t *testing.T, f *fixture, jobID string, lease *Lease) {
	t.Helper()
	if err := f.service.Start(context.Background(), AttemptRequest{Proof: NodeProof{Token: f.token},
		JobID: jobID, Attempt: lease.Attempt, Nonce: lease.Nonce}); err != nil {
		t.Fatal(err)
	}
	completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
		JobID: jobID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateFailed}}
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatal(err)
	}
}

func runningMTRJob(t *testing.T) (*fixture, model.DiagnosticJob, *Lease) {
	t.Helper()
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
	digest := advertise(t, f)
	job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
	lease := leaseJob(t, f, digest)
	if err := f.service.Start(context.Background(), AttemptRequest{Proof: NodeProof{Token: f.token},
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce}); err != nil {
		t.Fatal(err)
	}
	return f, job, lease
}

func failedMTRJob(t *testing.T) (*fixture, model.DiagnosticJob) {
	t.Helper()
	f, job, lease := runningMTRJob(t)
	failJob(t, f, job.ID, lease)
	return f, job
}

// expiredLeaseJob 返回一条租约已过期、尚未启动的作业，用于驱动租约回收分支。
func expiredLeaseJob(t *testing.T) (*fixture, model.DiagnosticJob) {
	t.Helper()
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
	digest := advertise(t, f)
	job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
	leaseJob(t, f, digest)
	execSQL(t, f, `UPDATE diagnostic_attempts SET leased_at=? WHERE job_id=?`, f.now.Add(-time.Hour), job.ID)
	return f, job
}

func advertiseIperfOnly(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, CapabilityDocument{Version: 1,
		Kinds: []string{model.DiagnosticKindIperf3}, State: "ready",
		Iperf3: &Iperf3Capability{Binary: "/usr/bin/iperf3", Version: "3.17.1",
			Protocols: []string{model.DiagnosticProtocolTCP}, Directions: []string{model.DiagnosticDirectionForward}},
		Maxima: CapabilityMaxima{Probes: 20, HopLimit: 32, DurationSeconds: 30, ParallelStreams: 4, BitrateMbps: 1000}}); err != nil {
		t.Fatal(err)
	}
}

func TestRetryJobFailureModes(t *testing.T) {
	cases := []struct {
		name     string
		sabotage func(*testing.T, *fixture, model.DiagnosticJob)
	}{
		{"policy unavailable", func(t *testing.T, f *fixture, _ model.DiagnosticJob) { dropTable(t, f, "diagnostic_policies") }},
		{"target unavailable", func(t *testing.T, f *fixture, _ model.DiagnosticJob) { dropTable(t, f, "diagnostic_targets") }},
		{"capability unavailable", func(t *testing.T, f *fixture, _ model.DiagnosticJob) { dropTable(t, f, "diagnostic_capabilities") }},
		{"kind not advertised", func(t *testing.T, f *fixture, _ model.DiagnosticJob) { advertiseIperfOnly(t, f) }},
		{"retry update rejected", func(t *testing.T, f *fixture, _ model.DiagnosticJob) {
			execSQL(t, f, `CREATE TRIGGER v221_retry BEFORE UPDATE ON diagnostic_jobs
				WHEN NEW.state='queued' AND OLD.completed_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		}},
		{"hop cleanup rejected", func(t *testing.T, f *fixture, job model.DiagnosticJob) {
			execSQL(t, f, `INSERT INTO diagnostic_hop_identities(job_id, hop_index, identity_json, created_at)
				VALUES(?, 1, 'sealed', ?)`, job.ID, f.now)
			execSQL(t, f, `CREATE TRIGGER v221_hop_delete BEFORE DELETE ON diagnostic_hop_identities WHEN 1
				BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		}},
		{"audit rejected", func(t *testing.T, f *fixture, _ model.DiagnosticJob) {
			execSQL(t, f, `CREATE TRIGGER v221_retry_audit BEFORE INSERT ON admin_audit_events
				WHEN NEW.action='diagnostic.job.retried' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		}},
		{"job vanished after update", func(t *testing.T, f *fixture, _ model.DiagnosticJob) {
			execSQL(t, f, `CREATE TRIGGER v221_retry_vanish AFTER UPDATE ON diagnostic_jobs
				WHEN NEW.state='queued' AND OLD.completed_at IS NOT NULL
				BEGIN DELETE FROM diagnostic_jobs WHERE id=NEW.id; END`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, job := failedMTRJob(t)
			tc.sabotage(t, f, job)
			if _, err := f.service.RetryJob(t.Context(), f.owner, job.ID); err == nil {
				t.Fatal("broken retry accepted")
			}
		})
	}
	t.Run("retried failed job", func(t *testing.T) {
		f, job := failedMTRJob(t)
		retried, err := f.service.RetryJob(t.Context(), f.owner, job.ID)
		if err != nil || retried.State != model.DiagnosticStateQueued {
			t.Fatalf("retry = %+v / %v", retried.State, err)
		}
	})
	t.Run("budget storage unavailable", func(t *testing.T) {
		f := newFixture(t)
		saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) {
			policy.Enabled = true
			policy.AllowedKinds = []string{model.DiagnosticKindIperf3}
			policy.DailyByteBudget = 1 << 30
		})
		target := createCoverageIperfTarget(t, f)
		advertiseIperf3(t, f)
		job, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(target))
		if err != nil {
			t.Fatal(err)
		}
		lease := leaseJob(t, f, mustPolicyDigest(t, f))
		failJob(t, f, job.ID, lease)
		dropTable(t, f, "diagnostic_budget_ledger")
		if _, err = f.service.RetryJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("retry error = %v", err)
		}
	})
}

// mustPolicyDigest 重新上报与夹具一致的能力文档，返回租约需要的摘要。
func mustPolicyDigest(t *testing.T, f *fixture) string {
	t.Helper()
	document, err := f.service.Capabilities(context.Background(), f.owner)
	if err != nil || len(document) != 1 {
		t.Fatalf("capabilities = %+v / %v", document, err)
	}
	return document[0].Digest
}

func TestCancelJobFailureModes(t *testing.T) {
	t.Run("forbidden actor", func(t *testing.T) {
		f, job := failedMTRJob(t)
		if err := f.service.CancelJob(t.Context(), Actor{UserID: "intruder", Role: "admin", SessionID: "session-intruder"}, job.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("cancel error = %v", err)
		}
	})
	t.Run("queued cancel rejected", func(t *testing.T) {
		f := newFixture(t)
		enableMTRPolicy(t, f, false)
		target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
		advertise(t, f)
		job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
		execSQL(t, f, `CREATE TRIGGER v221_cancel BEFORE UPDATE ON diagnostic_jobs
			WHEN NEW.state='cancelled' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.CancelJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cancel error = %v", err)
		}
	})
	t.Run("budget release rejected", func(t *testing.T) {
		f := newFixture(t)
		enableCoveragePolicy(t, f, model.DiagnosticKindIperf3)
		target := createCoverageIperfTarget(t, f)
		advertiseIperf3(t, f)
		job, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(target))
		if err != nil {
			t.Fatal(err)
		}
		dropTable(t, f, "diagnostic_budget_ledger")
		if err = f.service.CancelJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cancel error = %v", err)
		}
	})
	t.Run("running cancel rejected", func(t *testing.T) {
		f := newFixture(t)
		enableMTRPolicy(t, f, false)
		target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
		advertise(t, f)
		job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
		execSQL(t, f, `UPDATE diagnostic_jobs SET state='running' WHERE id=?`, job.ID)
		execSQL(t, f, `CREATE TRIGGER v221_cancel_running BEFORE UPDATE ON diagnostic_jobs
			WHEN NEW.cancel_requested=1 AND NEW.state='running' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.CancelJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cancel error = %v", err)
		}
	})
}

func TestReconcileFailureModes(t *testing.T) {
	t.Run("budget ledger unavailable", func(t *testing.T) {
		f := newFixture(t)
		dropTable(t, f, "diagnostic_budget_ledger")
		if err := f.service.Reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("queue expiry rejected", func(t *testing.T) {
		f := newFixture(t)
		enableMTRPolicy(t, f, false)
		target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
		advertise(t, f)
		job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
		execSQL(t, f, `UPDATE diagnostic_jobs SET cancel_requested=1 WHERE id=?`, job.ID)
		execSQL(t, f, `CREATE TRIGGER v221_expiry BEFORE UPDATE ON diagnostic_jobs
			WHEN NEW.state='cancelled' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.Reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("lease release rejected", func(t *testing.T) {
		f, _ := expiredLeaseJob(t)
		execSQL(t, f, `CREATE TRIGGER v221_release BEFORE UPDATE ON diagnostic_jobs
			WHEN NEW.state='queued' AND OLD.state='leased' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.Reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("attempt cleanup rejected", func(t *testing.T) {
		f, _ := expiredLeaseJob(t)
		execSQL(t, f, `CREATE TRIGGER v221_attempt_delete BEFORE DELETE ON diagnostic_attempts WHEN 1
			BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.Reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("heartbeat expiry rejected", func(t *testing.T) {
		f, job := expiredLeaseJob(t)
		execSQL(t, f, `UPDATE diagnostic_attempts SET state='running', heartbeat_at=? WHERE job_id=?`,
			f.now.Add(-time.Hour), job.ID)
		execSQL(t, f, `CREATE TRIGGER v221_unknown BEFORE UPDATE ON diagnostic_attempts
			WHEN NEW.state='unknown' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if err := f.service.Reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
}

func TestDeleteJobFailureModes(t *testing.T) {
	completed := func(t *testing.T) (*fixture, model.DiagnosticJob) {
		t.Helper()
		f, job, lease := runningMTRJob(t)
		progress(t, f, job.ID, lease)
		completeJob(t, f, job.ID, lease)
		return f, job
	}
	t.Run("metrics unavailable", func(t *testing.T) {
		f, job := completed(t)
		f.metrics.deleteFailure = ErrUnavailable
		if err := f.service.DeleteJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("delete error = %v", err)
		}
	})
	for _, table := range []string{"diagnostic_hop_identities", "diagnostic_attempts", "diagnostic_jobs"} {
		t.Run("cleanup rejected in "+table, func(t *testing.T) {
			f, job := completed(t)
			execSQL(t, f, `CREATE TRIGGER v221_delete BEFORE DELETE ON `+table+` WHEN 1
				BEGIN SELECT RAISE(ABORT, 'v221'); END`)
			if err := f.service.DeleteJob(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("delete error = %v", err)
			}
		})
	}
}

func TestProgressAndCompletionFailureModes(t *testing.T) {
	t.Run("progress rejects unknown proof", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		frame := ProgressFrame{Proof: NodeProof{Token: "bogus"}, ProgressRequest: diagnosticprotocol.ProgressRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1}}
		if _, err := f.service.Progress(t.Context(), frame); !errors.Is(err, ErrForbidden) {
			t.Fatalf("progress error = %v", err)
		}
	})
	t.Run("sample storage unavailable", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		f.metrics.writeFailure = ErrUnavailable
		frame := ProgressFrame{Proof: NodeProof{Token: f.token}, ProgressRequest: diagnosticprotocol.ProgressRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1,
			Samples: []diagnosticprotocol.Sample{{Metric: model.DiagnosticMetricMTRLatencyMS, Hop: 1,
				Stat: model.DiagnosticStatAvg, Value: 12.5}}}}
		if _, err := f.service.Progress(t.Context(), frame); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("progress error = %v", err)
		}
	})
	t.Run("invalid hop identity", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		frame := ProgressFrame{Proof: NodeProof{Token: f.token}, ProgressRequest: diagnosticprotocol.ProgressRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1,
			Hops: []HopIdentity{{Index: 0, Address: "93.184.216.34", Reached: true}}}}
		if _, err := f.service.Progress(t.Context(), frame); !errors.Is(err, ErrInvalid) {
			t.Fatalf("progress error = %v", err)
		}
	})
	t.Run("hop storage rejected", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		execSQL(t, f, `CREATE TRIGGER v221_hop_write BEFORE INSERT ON diagnostic_hop_identities WHEN 1
			BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		frame := ProgressFrame{Proof: NodeProof{Token: f.token}, ProgressRequest: diagnosticprotocol.ProgressRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1,
			Hops: []HopIdentity{{Index: 1, Address: "93.184.216.34", Reached: true}}}}
		if _, err := f.service.Progress(t.Context(), frame); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("progress error = %v", err)
		}
	})
	t.Run("completion rejects unknown proof", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		completion := Completion{Proof: NodeProof{Token: "bogus"}, CompletionRequest: diagnosticprotocol.CompletionRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
		if err := f.service.Complete(t.Context(), completion); !errors.Is(err, ErrForbidden) {
			t.Fatalf("completion error = %v", err)
		}
	})
	t.Run("conflicting completion", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		completeJob(t, f, job.ID, lease)
		completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateFailed}}
		if err := f.service.Complete(t.Context(), completion); !errors.Is(err, ErrConflict) {
			t.Fatalf("completion error = %v", err)
		}
	})
	t.Run("attempt update rejected", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		execSQL(t, f, `CREATE TRIGGER v221_attempt_complete BEFORE UPDATE ON diagnostic_attempts
			WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
		if err := f.service.Complete(t.Context(), completion); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("completion error = %v", err)
		}
	})
	t.Run("job update rejected", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		execSQL(t, f, `CREATE TRIGGER v221_job_complete BEFORE UPDATE ON diagnostic_jobs
			WHEN NEW.completed_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
		if err := f.service.Complete(t.Context(), completion); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("completion error = %v", err)
		}
	})
	t.Run("budget settlement rejected", func(t *testing.T) {
		f, job, lease := runningMTRJob(t)
		dropTable(t, f, "diagnostic_budget_ledger")
		completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
			JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded, ActualBytes: 4096}}
		if err := f.service.Complete(t.Context(), completion); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("completion error = %v", err)
		}
	})
}
