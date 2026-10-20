//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type failingMetrics struct{}

func (failingMetrics) WriteDiagnosticSamples(context.Context, store.DiagnosticSampleBatch) error {
	return errors.New("metrics write failed")
}

func (failingMetrics) ReadDiagnosticPoints(context.Context, store.DiagnosticQuery) ([]store.TimePoint, error) {
	return nil, errors.New("metrics read failed")
}

func (failingMetrics) DeleteDiagnosticJob(context.Context, string) error {
	return errors.New("metrics delete failed")
}

func TestHopIdentityBoundsAndDecoding(t *testing.T) {
	valid := HopIdentity{Index: 1, Address: "93.184.216.34", Hostname: "example.test"}
	if encoded, err := encodeHopIdentity(valid); err != nil || len(encoded) == 0 {
		t.Fatalf("encode = %s / %v", encoded, err)
	}
	cases := []HopIdentity{
		{Index: 0, Address: "93.184.216.34"},
		{Index: model.MaximumDiagnosticHops + 1, Address: "93.184.216.34"},
		{Index: 1, Address: "bad\naddress"},
		{Index: 1, Address: "93.184.216.34", Hostname: string(make([]byte, model.MaximumDiagnosticNameLength+1))},
	}
	for _, identity := range cases {
		if _, err := encodeHopIdentity(identity); !errors.Is(err, ErrInvalid) {
			t.Fatalf("identity %+v error = %v", identity, err)
		}
	}
	if validHopText("\xff\xfe", 8) || !validHopText("ok", 8) {
		t.Fatal("hop text validation is wrong")
	}
	if err := (transaction{}).storeHopIdentities("job", make([]HopIdentity, maximumFrameHops+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized hop frame error = %v", err)
	}

	f := newFixture(t)
	if _, err := f.database.DB.Exec(`INSERT INTO diagnostic_hop_identities(job_id, hop_index, identity_json, created_at)
		VALUES('bogus-job',1,x'00',?)`, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.hopIdentities(context.Background(), "bogus-job"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("corrupt hop error = %v", err)
	}
}

func TestIperfLifecycleSettlesBudgetAndCompletionGuards(t *testing.T) {
	f := newFixture(t)
	enableCoveragePolicy(t, f, model.DiagnosticKindIperf3)
	digest := advertiseIperf3(t, f)
	target := createCoverageIperfTarget(t, f)
	job, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(target))
	if err != nil || job.ReservedBytes <= 0 {
		t.Fatalf("job = %+v / %v", job, err)
	}
	lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: digest})
	if err != nil || lease == nil {
		t.Fatalf("lease = %+v / %v", lease, err)
	}
	attempt := AttemptRequest{Proof: NodeProof{Token: f.token}, JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce}

	beforeStart := tryProgress(t, f, job.ID, lease, 1)
	if !errors.Is(beforeStart, ErrConflict) {
		t.Fatalf("progress before start error = %v", beforeStart)
	}
	unstarted := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: model.DiagnosticStateSucceeded}}
	if err := f.service.Complete(context.Background(), unstarted); !errors.Is(err, ErrConflict) {
		t.Fatalf("complete before start error = %v", err)
	}
	if err := f.service.Start(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := tryProgress(t, f, job.ID, lease, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero sequence error = %v", err)
	}
	frame := ProgressFrame{Proof: NodeProof{Token: f.token}, ProgressRequest: diagnosticprotocol.ProgressRequest{
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, Sequence: 1,
		Samples: []diagnosticprotocol.Sample{{Metric: model.DiagnosticMetricIperfBytesPerSecond, Stat: model.DiagnosticStatAvg, Value: 100}}}}
	acknowledged, err := f.service.Progress(context.Background(), frame)
	if err != nil || acknowledged != 1 {
		t.Fatalf("progress = %d / %v", acknowledged, err)
	}
	tooManySamples := frame
	tooManySamples.Sequence = 2
	tooManySamples.Samples = make([]diagnosticprotocol.Sample, model.MaximumDiagnosticSampleBatch+1)
	if _, err := f.service.Progress(context.Background(), tooManySamples); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sample batch error = %v", err)
	}
	tooManyHops := frame
	tooManyHops.Sequence = 3
	tooManyHops.Samples = nil
	tooManyHops.Hops = make([]HopIdentity, maximumFrameHops+1)
	if _, err := f.service.Progress(context.Background(), tooManyHops); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hop batch error = %v", err)
	}
	f.service.metrics = failingMetrics{}
	failing := frame
	failing.Sequence = 4
	if _, err := f.service.Progress(context.Background(), failing); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("metrics failure error = %v", err)
	}
	f.service.metrics = f.metrics

	completion := Completion{Proof: NodeProof{Token: f.token}, CompletionRequest: diagnosticprotocol.CompletionRequest{
		JobID: job.ID, Attempt: lease.Attempt, Nonce: lease.Nonce, State: "nonsense"}}
	if err := f.service.Complete(context.Background(), completion); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid state error = %v", err)
	}
	completion.State = model.DiagnosticStateSucceeded
	completion.ActualBytes = 1234
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Heartbeat(context.Background(), attempt); !errors.Is(err, ErrConflict) {
		t.Fatalf("heartbeat after completion error = %v", err)
	}
	if err := f.service.Complete(context.Background(), completion); err != nil {
		t.Fatalf("idempotent completion failed: %v", err)
	}
	negative := completion
	negative.ActualBytes = -1
	if err := f.service.Complete(context.Background(), negative); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative bytes error = %v", err)
	}
}

func tryProgress(t *testing.T, f *fixture, jobID string, lease *Lease, sequence int64) error {
	t.Helper()
	_, err := f.service.Progress(context.Background(), ProgressFrame{Proof: NodeProof{Token: f.token},
		ProgressRequest: diagnosticprotocol.ProgressRequest{JobID: jobID, Attempt: lease.Attempt,
			Nonce: lease.Nonce, Sequence: sequence}})
	return err
}

func TestFinalStateBranches(t *testing.T) {
	started := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	attempt := storedAttempt{StartedAt: &started}
	cancelled := model.DiagnosticJob{CancelRequested: true}
	if state, err := finalState(cancelled, attempt, model.DiagnosticStateUnknown); err != nil || state != model.DiagnosticStateUnknown {
		t.Fatalf("unknown cancel = %s / %v", state, err)
	}
	if state, err := finalState(cancelled, attempt, model.DiagnosticStateSucceeded); err != nil || state != model.DiagnosticStateCancelled {
		t.Fatalf("cancel = %s / %v", state, err)
	}
	if _, err := finalState(model.DiagnosticJob{}, storedAttempt{}, model.DiagnosticStateSucceeded); !errors.Is(err, ErrConflict) {
		t.Fatalf("unstarted attempt error = %v", err)
	}
	if _, err := finalState(model.DiagnosticJob{}, attempt, "nonsense"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid state error = %v", err)
	}
	bound := bindSamples(model.DiagnosticJob{ID: "job", SourceNodeID: "node"},
		[]diagnosticprotocol.Sample{{Metric: model.DiagnosticMetricMTRLatencyMS}})
	if len(bound) != 1 || bound[0].JobID != "job" || bound[0].NodeID != "node" {
		t.Fatalf("bound samples = %+v", bound)
	}
}

func TestResultHelpersAndFailures(t *testing.T) {
	if !metricMatchesKind(model.DiagnosticKindMTR, model.DiagnosticMetricMTRLatencyMS) ||
		!metricMatchesKind(model.DiagnosticKindIperf3, model.DiagnosticMetricIperfBytesPerSecond) ||
		metricMatchesKind(model.DiagnosticKindMTR, model.DiagnosticMetricIperfBytes) ||
		metricMatchesKind("other", model.DiagnosticMetricMTRLatencyMS) {
		t.Fatal("metric matching is wrong")
	}
	created := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	completed := created.Add(2 * time.Minute)
	if _, to := resultWindow(model.DiagnosticJob{CreatedAt: created}); !to.Equal(created.Add(time.Hour)) {
		t.Fatal("open window is wrong")
	}
	if _, to := resultWindow(model.DiagnosticJob{CreatedAt: created, CompletedAt: &completed}); !to.Equal(completed.Add(time.Minute)) {
		t.Fatal("closed window is wrong")
	}
	units := map[string]string{model.DiagnosticMetricMTRLatencyMS: "milliseconds",
		model.DiagnosticMetricMTRLossPercent: "percent", model.DiagnosticMetricIperfBytesPerSecond: "bytes_per_second",
		model.DiagnosticMetricIperfBytes: "bytes", "other": "count"}
	for metric, unit := range units {
		if diagnosticUnit(metric) != unit {
			t.Fatalf("unit for %s = %s", metric, diagnosticUnit(metric))
		}
	}
	if resultError(nil) != nil || !errors.Is(resultError(metricquery.ErrInvalid), ErrInvalid) ||
		!errors.Is(resultError(metricquery.ErrCapacity), ErrInvalid) || !errors.Is(resultError(metricquery.ErrArithmetic), ErrUnavailable) ||
		!errors.Is(resultError(errors.New("boom")), ErrUnavailable) {
		t.Fatal("result error mapping is wrong")
	}

	f := newFixture(t)
	enableCoveragePolicy(t, f, model.DiagnosticKindIperf3)
	advertiseIperf3(t, f)
	target := createCoverageIperfTarget(t, f)
	job, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(target))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Results(context.Background(), f.owner,
		ResultQuery{JobID: job.ID, Metric: model.DiagnosticMetricIperfBytesPerSecond, Limit: -1}); err != nil {
		t.Fatalf("negative limit error = %v", err)
	}
	if _, err := f.service.Results(context.Background(), f.owner,
		ResultQuery{JobID: job.ID, Metric: model.DiagnosticMetricIperfBytesPerSecond, Limit: 99_999}); err != nil {
		t.Fatalf("oversized limit error = %v", err)
	}
	f.service.metrics = failingMetrics{}
	if _, err := f.service.Results(context.Background(), f.owner,
		ResultQuery{JobID: job.ID, Metric: model.DiagnosticMetricIperfBytesPerSecond}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("metrics failure error = %v", err)
	}
}
