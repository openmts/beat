//go:build linux

package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentdiagnostics"
	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// wireTransport 把真实的 Agent 诊断客户端直接接到服务端服务上，只跳过 net/http
// 的 socket 绑定；路径、请求体、信封与错误映射仍然走生产代码。
type wireTransport struct {
	t         *testing.T
	service   *Service
	token     string
	paths     []string
	lastError error
}

func (transport *wireTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.t.Helper()
	content, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/agent-diagnostics/")
	transport.paths = append(transport.paths, path)
	proof := NodeProof{Token: strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")}
	return transport.dispatch(request.Context(), path, proof, content)
}

func (transport *wireTransport) dispatch(ctx context.Context, path string, proof NodeProof, content []byte) (*http.Response, error) {
	switch path {
	case "capability":
		var document CapabilityDocument
		if err := json.Unmarshal(content, &document); err != nil {
			return diagnosticWireEnvelope(map[string]string{"digest": ""}, http.StatusBadRequest), nil
		}
		digest, err := transport.service.Advertise(ctx, proof, document)
		return diagnosticWireEnvelope(map[string]string{"digest": digest}, transport.status(err)), nil
	case "lease":
		var request diagnosticprotocol.LeaseRequest
		if err := json.Unmarshal(content, &request); err != nil {
			return diagnosticWireEnvelope(nil, http.StatusBadRequest), nil
		}
		lease, err := transport.service.Lease(ctx, LeaseRequest{Proof: proof, CapabilityDigest: request.CapabilityDigest})
		return diagnosticWireEnvelope(lease, transport.status(err)), nil
	default:
		return transport.dispatchAttempt(ctx, path, proof, content)
	}
}

func (transport *wireTransport) dispatchAttempt(ctx context.Context, path string, proof NodeProof, content []byte) (*http.Response, error) {
	switch {
	case strings.HasSuffix(path, "/start"):
		var request diagnosticprotocol.AttemptRequest
		if err := json.Unmarshal(content, &request); err != nil {
			return diagnosticWireEnvelope(nil, http.StatusBadRequest), nil
		}
		err := transport.service.Start(ctx, AttemptRequest{Proof: proof, JobID: request.JobID,
			Attempt: request.Attempt, Nonce: request.Nonce})
		return diagnosticWireEnvelope(map[string]bool{"acknowledged": err == nil}, transport.status(err)), nil
	case strings.HasSuffix(path, "/heartbeat"):
		var request diagnosticprotocol.AttemptRequest
		err := json.Unmarshal(content, &request)
		if err != nil {
			return diagnosticWireEnvelope(nil, http.StatusBadRequest), nil
		}
		cancel, heartbeatErr := transport.service.Heartbeat(ctx, AttemptRequest{Proof: proof, JobID: request.JobID,
			Attempt: request.Attempt, Nonce: request.Nonce})
		return diagnosticWireEnvelope(map[string]bool{"cancel_requested": cancel}, transport.status(heartbeatErr)), nil
	case strings.HasSuffix(path, "/progress"):
		var request diagnosticprotocol.ProgressRequest
		err := json.Unmarshal(content, &request)
		if err != nil {
			return diagnosticWireEnvelope(nil, http.StatusBadRequest), nil
		}
		sequence, progressErr := transport.service.Progress(ctx, ProgressFrame{Proof: proof, ProgressRequest: request})
		return diagnosticWireEnvelope(map[string]int64{"sequence": sequence}, transport.status(progressErr)), nil
	case strings.HasSuffix(path, "/complete"):
		var request diagnosticprotocol.CompletionRequest
		err := json.Unmarshal(content, &request)
		if err != nil {
			return diagnosticWireEnvelope(nil, http.StatusBadRequest), nil
		}
		completeErr := transport.service.Complete(ctx, Completion{Proof: proof, CompletionRequest: request})
		return diagnosticWireEnvelope(map[string]bool{"acknowledged": completeErr == nil}, transport.status(completeErr)), nil
	default:
		transport.t.Fatalf("unexpected agent path %q", path)
		return nil, nil
	}
}

// diagnosticWireStatus 复刻真实处理器把服务端错误映射成 HTTP 状态码的行为。
func (transport *wireTransport) status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	transport.lastError = err
	switch {
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrCapability):
		return http.StatusPreconditionFailed
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrBudget):
		return http.StatusTooManyRequests
	default:
		return http.StatusServiceUnavailable
	}
}

func diagnosticWireEnvelope(data any, status int) *http.Response {
	encoded, _ := json.Marshal(map[string]any{"code": status, "data": data})
	header := http.Header{"Content-Type": []string{"application/json"}}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(string(encoded)))}
}

func agentDiagnosticClient(t *testing.T, f *fixture) (*agentdiagnostics.Client, *wireTransport) {
	t.Helper()
	transport := &wireTransport{t: t, service: f.service, token: f.token}
	client, err := agentdiagnostics.NewClient(agentdiagnostics.ClientConfig{ServerURL: "https://beat.example.test",
		Token: f.token, Transport: transport})
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	return client, transport
}

func agentCapability(t *testing.T) diagnosticprotocol.CapabilityDocument {
	t.Helper()
	config := agentdiagnostics.Config{Enabled: true, MTRPath: "/usr/bin/mtr", Iperf3Path: "/usr/bin/iperf3",
		StateDir: t.TempDir(), AllowedTargets: []string{"93.184.216.34"},
		Maxima:            agentdiagnostics.Maxima{Probes: 20, HopLimit: 32, DurationSeconds: 30, ParallelStreams: 4, BitrateMbps: 1000},
		MaximumConcurrent: 1, AllowUDP: true, AllowReverse: true}
	if err := agentdiagnostics.ValidateConfig(config); err != nil {
		t.Fatalf("ValidateConfig error = %v", err)
	}
	return config.Capability("ready")
}

func TestAgentClientCompletesAnApprovedJob(t *testing.T) {
	f := newFixture(t)
	policy := enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "Endpoint", "diagnostics.example.test", false)
	client, transport := agentDiagnosticClient(t, f)
	ctx := context.Background()
	digest, err := client.Advertise(ctx, agentCapability(t))
	if err != nil {
		t.Fatalf("Advertise error = %v", err)
	}
	job := createMTRJob(t, f, policy, target)
	lease, err := client.Lease(ctx, digest)
	if err != nil || lease == nil {
		t.Fatalf("Lease = %+v (%v)", lease, err)
	}
	if lease.JobID != job.ID || lease.Payload.Destination != "93.184.216.34" {
		t.Fatalf("lease = %+v", lease)
	}
	attempt := diagnosticprotocol.AttemptRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt}
	if err := client.Start(ctx, attempt); err != nil {
		t.Fatalf("Start error = %v", err)
	}
	cancel, err := client.Heartbeat(ctx, attempt)
	if err != nil || cancel {
		t.Fatalf("Heartbeat = %v (%v)", cancel, err)
	}
	for sequence := int64(1); sequence <= 3; sequence++ {
		frame := diagnosticprotocol.ProgressRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt,
			Sequence: sequence, Samples: []diagnosticprotocol.Sample{{Metric: model.DiagnosticMetricMTRLatencyMS,
				Hop: int(sequence), Stat: model.DiagnosticStatLast, Value: float64(sequence)}},
			Hops: []diagnosticprotocol.HopIdentity{{Index: int(sequence), Address: "93.184.216.34", Reached: true}}}
		acknowledged, ackErr := client.Progress(ctx, frame)
		if ackErr != nil || acknowledged != sequence {
			t.Fatalf("Progress %d = %d (%v)", sequence, acknowledged, ackErr)
		}
	}
	if err := client.Complete(ctx, diagnosticprotocol.CompletionRequest{JobID: lease.JobID, Nonce: lease.Nonce,
		Attempt: lease.Attempt, State: model.DiagnosticStateSucceeded, ActualBytes: 4096}); err != nil {
		t.Fatalf("Complete error = %v (server error %v)", err, transport.lastError)
	}
	detail, err := f.service.Job(ctx, f.owner, job.ID)
	if err != nil || detail.Job.State != model.DiagnosticStateSucceeded || detail.Job.ActualBytes != 4096 {
		t.Fatalf("job detail = %+v (%v)", detail.Job, err)
	}
	if len(detail.Hops) != 3 || detail.Hops[2].Address != "93.184.216.34" {
		t.Fatalf("decrypted hops = %+v", detail.Hops)
	}
	f.metrics.mu.Lock()
	batches := append([]store.DiagnosticSampleBatch(nil), f.metrics.batches...)
	f.metrics.mu.Unlock()
	if len(batches) != 3 {
		t.Fatalf("mts batches = %d", len(batches))
	}
	for index, batch := range batches {
		if batch.JobID != job.ID || batch.NodeID != f.nodeID || len(batch.Samples) != 1 ||
			batch.Samples[0].JobID != job.ID || batch.Samples[0].Value != float64(index+1) {
			t.Fatalf("mts batch %d = %+v", index, batch)
		}
	}
	f.metrics.mu.Lock()
	f.metrics.points = []store.TimePoint{{Timestamp: f.now, Value: 12.5}}
	f.metrics.mu.Unlock()
	series, err := f.service.Results(ctx, f.owner, ResultQuery{JobID: job.ID, Metric: model.DiagnosticMetricMTRLatencyMS,
		Hop: 1, Stat: model.DiagnosticStatLast})
	if err != nil || len(series.Points) != 1 || series.Unit != "milliseconds" {
		t.Fatalf("Results = %+v (%v)", series, err)
	}
	want := []string{"capability", "lease", "attempts/" + job.ID + "/start", "attempts/" + job.ID + "/heartbeat",
		"attempts/" + job.ID + "/progress", "attempts/" + job.ID + "/progress", "attempts/" + job.ID + "/progress",
		"attempts/" + job.ID + "/complete"}
	if len(transport.paths) != len(want) {
		t.Fatalf("paths = %+v", transport.paths)
	}
	for index, path := range want {
		if transport.paths[index] != path {
			t.Errorf("path %d = %q, want %q", index, transport.paths[index], path)
		}
	}
}

func TestAgentClientReportsCancellationAndStaleNonce(t *testing.T) {
	f := newFixture(t)
	policy := enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "Endpoint", "diagnostics.example.test", false)
	client, _ := agentDiagnosticClient(t, f)
	ctx := context.Background()
	digest, err := client.Advertise(ctx, agentCapability(t))
	if err != nil {
		t.Fatalf("Advertise error = %v", err)
	}
	job := createMTRJob(t, f, policy, target)
	lease, err := client.Lease(ctx, digest)
	if err != nil || lease == nil {
		t.Fatalf("Lease = %+v (%v)", lease, err)
	}
	attempt := diagnosticprotocol.AttemptRequest{JobID: lease.JobID, Nonce: lease.Nonce, Attempt: lease.Attempt}
	if err := client.Start(ctx, attempt); err != nil {
		t.Fatalf("Start error = %v", err)
	}
	if err := f.service.CancelJob(ctx, f.owner, job.ID); err != nil {
		t.Fatalf("CancelJob error = %v", err)
	}
	cancel, err := client.Heartbeat(ctx, attempt)
	if err != nil || !cancel {
		t.Fatalf("Heartbeat = %v (%v)", cancel, err)
	}
	if err := client.Complete(ctx, diagnosticprotocol.CompletionRequest{JobID: lease.JobID, Nonce: lease.Nonce,
		Attempt: lease.Attempt, State: model.DiagnosticStateCancelled}); err != nil {
		t.Fatalf("Complete error = %v", err)
	}
	detail, err := f.service.Job(ctx, f.owner, job.ID)
	if err != nil || detail.Job.State != model.DiagnosticStateCancelled {
		t.Fatalf("job detail = %+v (%v)", detail.Job, err)
	}
	stale := attempt
	stale.Nonce = strings.Repeat("0", 64)
	if cancel, err := client.Heartbeat(ctx, stale); err == nil || cancel {
		t.Fatalf("stale nonce heartbeat = %v (%v)", cancel, err)
	}
}
