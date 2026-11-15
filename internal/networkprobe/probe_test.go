package networkprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunTCPMeasuresLocalListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	config := Config{
		Targets: []Target{
			{Carrier: CarrierTelecom, Label: "电信", Host: "127.0.0.1", Port: port, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
			{Carrier: CarrierUnicom, Label: "联通", Host: "127.0.0.1", Port: port, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
			{Carrier: CarrierMobile, Label: "移动", Host: "127.0.0.1", Port: port, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
		},
		Probe: ProbeSettings{Count: 3, IntervalMs: 50, TimeoutMs: 500},
	}
	result := RunTCP(context.Background(), config)
	if len(result.Targets) != 3 {
		t.Fatalf("target results = %d, want 3", len(result.Targets))
	}
	for _, target := range result.Targets {
		if target.Status != "ok" || target.Loss.Sent != 3 || target.Loss.Received != 3 || target.Loss.Percent == nil || *target.Loss.Percent != 0 {
			t.Fatalf("unexpected loss result: %+v", target)
		}
		if !target.Latency.Available || target.Latency.SampleCount != 3 || target.Latency.AvgMs == nil || target.Latency.P95Ms == nil {
			t.Fatalf("unexpected latency result: %+v", target.Latency)
		}
		if len(target.Samples) != 3 {
			t.Fatalf("samples = %d, want 3", len(target.Samples))
		}
		for _, sample := range target.Samples {
			if sample.Outcome != OutcomeSuccess || sample.RTTMs == nil {
				t.Fatalf("unexpected sample: %+v", sample)
			}
		}
	}
	if result.DurationMs > 10_000 {
		t.Fatalf("probe took too long: %dms", result.DurationMs)
	}
}

func TestProbeTCPReportsPartialLossAndLatencyFromSuccessfulSamples(t *testing.T) {
	calls := 0
	dialer := func(context.Context, string, string, time.Duration) (net.Conn, error) {
		calls++
		if calls == 2 {
			return nil, context.DeadlineExceeded
		}
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	result := probeTCP(context.Background(), Target{
		Carrier: CarrierTelecom, Label: "电信", Host: "127.0.0.1", Port: 443,
		Protocol: ProtocolTCP, AddressFamily: FamilyIPv4,
	}, ProbeSettings{Count: 3, IntervalMs: 1, TimeoutMs: 100}, dialer)

	if result.Status != "degraded" || !result.Loss.Available || result.Loss.Sent != 3 || result.Loss.Received != 2 || result.Loss.Lost != 1 || result.Loss.Failed != 0 {
		t.Fatalf("unexpected loss metrics: %+v", result)
	}
	if result.Loss.Percent == nil || math.Abs(*result.Loss.Percent-33.333333) > 0.001 {
		t.Fatalf("loss percent = %v, want 33.333", result.Loss.Percent)
	}
	if !result.Latency.Available || result.Latency.SampleCount != 2 || result.Latency.AvgMs == nil {
		t.Fatalf("unexpected latency metrics: %+v", result.Latency)
	}
	if len(result.Samples) != 3 || result.Samples[0].Outcome != OutcomeSuccess || result.Samples[1].Outcome != OutcomeLost || result.Samples[2].Outcome != OutcomeSuccess {
		t.Fatalf("unexpected samples: %+v", result.Samples)
	}
	if result.Samples[1].RTTMs != nil {
		t.Fatalf("lost sample has latency: %v", *result.Samples[1].RTTMs)
	}
}

func TestProbeTCPReportsAllLostWithoutLatency(t *testing.T) {
	dialer := func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}
	result := probeTCP(context.Background(), Target{
		Carrier: CarrierUnicom, Label: "联通", Host: "127.0.0.1", Port: 443,
		Protocol: ProtocolTCP, AddressFamily: FamilyIPv4,
	}, ProbeSettings{Count: 4, IntervalMs: 1, TimeoutMs: 100}, dialer)

	if result.Status != "failed" || !result.Loss.Available || result.Loss.Percent == nil || *result.Loss.Percent != 100 {
		t.Fatalf("unexpected loss metrics: %+v", result.Loss)
	}
	if result.Loss.Received != 0 || result.Loss.Lost != 4 || result.Loss.Failed != 0 {
		t.Fatalf("unexpected loss counters: %+v", result.Loss)
	}
	if result.Latency.Available || result.Latency.SampleCount != 0 || result.Latency.MinMs != nil || result.Latency.AvgMs != nil || result.Latency.MaxMs != nil || result.Latency.P50Ms != nil || result.Latency.P95Ms != nil || result.Latency.JitterMs != nil {
		t.Fatalf("all-lost latency should be unavailable: %+v", result.Latency)
	}
	if len(result.Samples) != 4 {
		t.Fatalf("samples = %d, want 4", len(result.Samples))
	}
	for _, sample := range result.Samples {
		if sample.Outcome != OutcomeLost || sample.RTTMs != nil {
			t.Fatalf("unexpected lost sample: %+v", sample)
		}
	}
}

func TestAllLostResultSerializesLatencyAsNull(t *testing.T) {
	dialer := func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}
	result := probeTCP(context.Background(), Target{
		Carrier: CarrierMobile, Label: "移动", Host: "127.0.0.1", Port: 443,
		Protocol: ProtocolTCP, AddressFamily: FamilyIPv4,
	}, ProbeSettings{Count: 1, IntervalMs: 1, TimeoutMs: 100}, dialer)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	if !strings.Contains(output, `"loss":{"available":true,"sent":1,"received":0,"lost":1,"failed":0,"percent":100}`) {
		t.Fatalf("unexpected loss JSON: %s", output)
	}
	if !strings.Contains(output, `"latency":{"available":false,"sampleCount":0,"minMs":null,"avgMs":null,"maxMs":null,"p50Ms":null,"p95Ms":null,"jitterMs":null}`) {
		t.Fatalf("unavailable latency must encode as null: %s", output)
	}
	if !strings.Contains(output, `"samples":[{"index":1,"outcome":"lost","rttMs":null`) {
		t.Fatalf("lost sample must encode rttMs as null: %s", output)
	}
}

func TestProbeTCPClassifiesConnectionFailureSeparatelyFromLoss(t *testing.T) {
	dialer := func(context.Context, string, string, time.Duration) (net.Conn, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}
	result := probeTCP(context.Background(), Target{
		Carrier: CarrierMobile, Label: "移动", Host: "127.0.0.1", Port: 443,
		Protocol: ProtocolTCP, AddressFamily: FamilyIPv4,
	}, ProbeSettings{Count: 2, IntervalMs: 1, TimeoutMs: 100}, dialer)

	if result.Status != "failed" || !result.Loss.Available || result.Loss.Percent == nil || *result.Loss.Percent != 0 {
		t.Fatalf("unexpected loss metrics: %+v", result.Loss)
	}
	if result.Loss.Lost != 0 || result.Loss.Failed != 2 || result.Latency.Available {
		t.Fatalf("connection failures were misclassified: %+v", result)
	}
	for _, sample := range result.Samples {
		if sample.Outcome != OutcomeFailed || sample.RTTMs != nil {
			t.Fatalf("unexpected failed sample: %+v", sample)
		}
	}
}

func TestRunTCPReportsResolutionFailureWithoutMetrics(t *testing.T) {
	config := Config{
		Targets: []Target{
			{Carrier: CarrierTelecom, Host: "invalid.invalid", Port: 443, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
			{Carrier: CarrierUnicom, Host: "invalid.invalid", Port: 443, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
			{Carrier: CarrierMobile, Host: "invalid.invalid", Port: 443, Protocol: ProtocolTCP, AddressFamily: FamilyIPv4},
		},
		Probe: ProbeSettings{Count: 1, IntervalMs: 50, TimeoutMs: 100},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := RunTCP(ctx, config)
	for _, target := range result.Targets {
		if target.Status != "failed" || len(target.Errors) == 0 {
			t.Fatalf("unexpected failed target result: %+v", target)
		}
		if target.Loss.Available || target.Loss.Percent != nil || target.Loss.Sent != 0 {
			t.Fatalf("resolution failure should make loss unavailable: %+v", target.Loss)
		}
		if target.Latency.Available || target.Latency.SampleCount != 0 || target.Latency.AvgMs != nil || len(target.Samples) != 0 {
			t.Fatalf("resolution failure should make latency unavailable: %+v", target)
		}
	}
}

func TestClassifyDialError(t *testing.T) {
	if outcome := classifyDialError(context.DeadlineExceeded); outcome != OutcomeLost {
		t.Fatalf("deadline outcome = %q, want %q", outcome, OutcomeLost)
	}
	if outcome := classifyDialError(errors.New("connection refused")); outcome != OutcomeFailed {
		t.Fatalf("refused outcome = %q, want %q", outcome, OutcomeFailed)
	}
}
