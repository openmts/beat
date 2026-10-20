package agentdiagnostics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

type fakeGateway struct {
	mu sync.Mutex

	advertised   []diagnosticprotocol.CapabilityDocument
	started      []diagnosticprotocol.AttemptRequest
	progress     []diagnosticprotocol.ProgressRequest
	completions  []diagnosticprotocol.CompletionRequest
	leases       []string
	lease        *diagnosticprotocol.Lease
	digest       string
	cancel       bool
	leaseErr     error
	startErr     error
	progressErr  error
	completeErr  error
	heartbeatErr error
	hearts       int
}

func (gateway *fakeGateway) Advertise(_ context.Context, document diagnosticprotocol.CapabilityDocument) (string, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.advertised = append(gateway.advertised, document)
	if gateway.digest == "" {
		return document.Digest()
	}
	return gateway.digest, nil
}

func (gateway *fakeGateway) Lease(_ context.Context, digest string) (*diagnosticprotocol.Lease, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.leases = append(gateway.leases, digest)
	if gateway.leaseErr != nil {
		return nil, gateway.leaseErr
	}
	return gateway.lease, nil
}

func (gateway *fakeGateway) Start(_ context.Context, attempt diagnosticprotocol.AttemptRequest) error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.started = append(gateway.started, attempt)
	return gateway.startErr
}

func (gateway *fakeGateway) Heartbeat(_ context.Context, _ diagnosticprotocol.AttemptRequest) (bool, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.hearts++
	return gateway.cancel, gateway.heartbeatErr
}

func (gateway *fakeGateway) Progress(_ context.Context, frame diagnosticprotocol.ProgressRequest) (int64, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.progress = append(gateway.progress, frame)
	if gateway.progressErr != nil {
		return 0, gateway.progressErr
	}
	return frame.Sequence, nil
}

func (gateway *fakeGateway) Complete(_ context.Context, completion diagnosticprotocol.CompletionRequest) error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.completions = append(gateway.completions, completion)
	return gateway.completeErr
}

func (gateway *fakeGateway) snapshot() fakeGatewayState {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return fakeGatewayState{advertised: len(gateway.advertised), started: len(gateway.started), hearts: gateway.hearts,
		leases:      len(gateway.leases),
		progress:    append([]diagnosticprotocol.ProgressRequest(nil), gateway.progress...),
		completions: append([]diagnosticprotocol.CompletionRequest(nil), gateway.completions...)}
}

type fakeGatewayState struct {
	advertised  int
	started     int
	leases      int
	hearts      int
	progress    []diagnosticprotocol.ProgressRequest
	completions []diagnosticprotocol.CompletionRequest
}

type fakeExecutor struct {
	kind   string
	frames []batch
	result outcome
	err    error
}

func (executor fakeExecutor) Kind() string { return executor.kind }

func (executor fakeExecutor) Execute(_ context.Context, _ jobSpec, sink sink) (outcome, error) {
	for _, frame := range executor.frames {
		if err := sink(frame); err != nil {
			return outcome{}, err
		}
	}
	return executor.result, executor.err
}

// blockingExecutor 只在上下文取消后返回，用来证明取消会终止执行。
type blockingExecutor struct{ kind string }

func (executor blockingExecutor) Kind() string { return executor.kind }

func (executor blockingExecutor) Execute(ctx context.Context, _ jobSpec, _ sink) (outcome, error) {
	<-ctx.Done()
	return terminalOutcome(model.DiagnosticStateCancelled, "canceled"), nil
}

func diagnosticRunner(t *testing.T, gateway Gateway, executors map[string]executor) *Runner {
	t.Helper()
	config := validConfig(t)
	config.AllowedTargets = []string{"8.8.8.8", "10.0.0.0/8"}
	config.AllowPrivateTargets = true
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: gateway})
	if err != nil {
		t.Fatalf("NewRunner error = %v", err)
	}
	runner.settings.executors = executors
	return runner
}

func benchmarkLease(destination string) *diagnosticprotocol.Lease {
	return &diagnosticprotocol.Lease{JobID: "job-1", Attempt: 1, Nonce: "nonce", ExpiresAt: time.Now().Add(time.Minute).UnixMilli(),
		Payload: diagnosticprotocol.JobPayload{Kind: model.DiagnosticKindMTR, Destination: destination,
			Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
			IPFamily: model.IPFamilyIPv4, Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops}}
}

func hopSamples(count int) []diagnosticprotocol.Sample {
	samples := make([]diagnosticprotocol.Sample, 0, count)
	for index := range count {
		samples = append(samples, diagnosticprotocol.Sample{Metric: model.DiagnosticMetricMTRLatencyMS, Hop: 1,
			Stat: model.DiagnosticStatLast, Value: float64(index)})
	}
	return samples
}
