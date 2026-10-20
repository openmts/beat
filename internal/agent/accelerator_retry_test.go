package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/acceleratorcollect"
	"github.com/beat/backend/internal/model"
)

type gpuRetryExecutor struct{}

func (gpuRetryExecutor) Run(_ context.Context, vendor string) ([]byte, error) {
	if vendor == "amd" {
		return nil, os.ErrNotExist
	}
	return []byte("0000:01:00.0, Fixture, 1, 42, 1, 2, 50, 1, 2\n"), nil
}

type gpuRetryCollector struct{ count int }

func (c *gpuRetryCollector) Collect(context.Context) (model.NodeMetrics, error) {
	c.count++
	return model.NodeMetrics{CapturedAt: time.Now().UTC(), CPU: float64(c.count)}, nil
}

type gpuRetryHealer struct{ cancel context.CancelFunc }

func (h gpuRetryHealer) SelfHeal(context.Context, string) error { h.cancel(); return nil }

func TestAcceleratorPolicyChangesDoNotRewriteRetriesOrBlockHostCollection(t *testing.T) {
	for _, change := range []string{"disabled", "cadence"} {
		t.Run(change, func(t *testing.T) { testGPUPolicyRetry(t, change) })
	}
}

func testGPUPolicyRetry(t *testing.T, change string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	gpu, err := acceleratorcollect.New(acceleratorcollect.Config{Enabled: true, SampleIntervalSeconds: 5}, gpuRetryExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	policy := model.DefaultAcceleratorPolicy()
	policy.Configured = true
	policy.Enabled = true
	policy.Revision = 1
	policy.SampleIntervalSeconds = 5
	if err = gpu.ApplyPolicy(policy); err != nil {
		t.Fatal(err)
	}
	collected := make(chan error, 1)
	go func() { collected <- gpu.Run(ctx) }()
	waitGPUFixture(t, ctx, gpu)
	updated := policy
	updated.Revision = 2
	if change == "disabled" {
		updated.Enabled = false
	} else {
		updated.SampleIntervalSeconds = 10
	}
	capture := &gpuRetryCapture{policy: updated}
	server := httptest.NewServer(http.HandlerFunc(capture.serve))
	defer server.Close()
	reporter, err := NewHTTPReporter(Config{ServerURL: server.URL, AgentToken: "fixture", NodeName: "gpu", AdvertisedHost: "host", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	reporter.WithAcceleratorPolicyReceiver(gpu.ApplyPolicy)
	collector := &gpuRetryCollector{}
	runner := NewRunner(RunnerOptions{Interval: time.Second, Collector: collector, Accelerators: gpu, Reporter: reporter, SelfHealer: gpuRetryHealer{cancel: cancel}})
	if err = runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-collected; err != context.Canceled {
		t.Fatal("collector shutdown", err)
	}
	capture.assert(t, change)
	if collector.count < 2 {
		t.Fatal("host collection stayed frozen")
	}
}

func waitGPUFixture(t *testing.T, ctx context.Context, gpu *acceleratorcollect.Collector) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for gpu.Snapshot() == nil {
		select {
		case <-ctx.Done():
			t.Fatal("fixture did not collect")
		case <-ticker.C:
		}
	}
}

type gpuRetryCapture struct {
	mu        sync.Mutex
	policy    model.AcceleratorPolicy
	bodies    [][]byte
	reports   []model.AgentReport
	decodeErr error
}

func (c *gpuRetryCapture) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	var report model.AgentReport
	if err == nil {
		err = json.Unmarshal(raw, &report)
	}
	c.mu.Lock()
	c.bodies = append(c.bodies, raw)
	c.reports = append(c.reports, report)
	index := len(c.bodies)
	c.decodeErr = err
	c.mu.Unlock()
	ack := model.AgentReportACK{ProtocolVersion: 2, SampleID: report.SampleID, CapturedAt: report.CapturedAt, AcceleratorPolicy: &c.policy}
	status := http.StatusServiceUnavailable
	if index == 1 {
		ack.ErrorCode = "storage_unavailable"
		ack.Retryable = true
	}
	if index == 2 {
		status = http.StatusConflict
		ack.ErrorCode = "accelerator_policy_changed"
	}
	if index >= 3 {
		status = http.StatusCreated
		ack.ContactAccepted = true
		ack.MetricsAccepted = true
		now := time.Now().UTC()
		ack.ContactAt = &now
		ack.SSHPublicKey = "fixture"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": ack}); err != nil {
		c.mu.Lock()
		c.decodeErr = err
		c.mu.Unlock()
	}
}

func (c *gpuRetryCapture) assert(t *testing.T, change string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.decodeErr != nil || len(c.bodies) < 3 {
		t.Fatal("retry flow failed", len(c.bodies), c.decodeErr)
	}
	if !bytes.Equal(c.bodies[0], c.bodies[1]) {
		t.Fatal("same pending sample changed after policy ACK")
	}
	if c.reports[0].SampleID == c.reports[2].SampleID || c.reports[2].Metrics.CPU <= c.reports[0].Metrics.CPU {
		t.Fatal("old sample not discarded on policy conflict")
	}
	if gpu := c.reports[2].Accelerators; gpu != nil {
		if gpu.PolicyRevision != 2 {
			t.Fatal("old GPU policy reused")
		}
		if change == "disabled" && gpu.State != "disabled" {
			t.Fatal("disabled policy still collected")
		}
		if change == "cadence" && gpu.CadenceMS != 10000 {
			t.Fatal("new collector cadence not reported")
		}
	}
}
