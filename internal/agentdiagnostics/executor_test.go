package agentdiagnostics

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

func TestSpecFromLeaseEnforcesLocalMaxima(t *testing.T) {
	config := validConfig(t)
	lease := benchmarkLease("8.8.8.8")
	if _, err := specFromLease(config, lease, "/tmp"); err != nil {
		t.Fatalf("mtr spec error = %v", err)
	}
	lease.Payload.Probes = config.Maxima.Probes + 1
	if _, err := specFromLease(config, lease, "/tmp"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("probe overflow error = %v", err)
	}
	lease = benchmarkLease("8.8.8.8")
	lease.Payload.Kind = model.DiagnosticKindIperf3
	lease.Payload.DurationSeconds = config.Maxima.DurationSeconds + 1
	lease.Payload.ParallelStreams = 1
	lease.Payload.BitrateMbps = 10
	lease.Payload.Port = 5201
	if _, err := specFromLease(config, lease, "/tmp"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("duration overflow error = %v", err)
	}
	lease.Payload.DurationSeconds = 5
	spec, err := specFromLease(config, lease, "/tmp")
	if err != nil || spec.Port != 5201 || spec.Directory != "/tmp" {
		t.Fatalf("iperf3 spec = %+v (%v)", spec, err)
	}
	lease.Payload.Kind = "unknown"
	if _, err := specFromLease(config, lease, "/tmp"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("unknown kind error = %v", err)
	}
}

func TestProgressBatchesSplitsFrames(t *testing.T) {
	if batches := progressBatches(batch{}); batches != nil {
		t.Fatalf("empty frame batches = %+v", batches)
	}
	if batches := progressBatches(batch{Hops: []diagnosticprotocol.HopIdentity{{Index: 1}}}); len(batches) != 1 || batches[0] != nil {
		t.Fatalf("hop-only batches = %+v", batches)
	}
	batches := progressBatches(batch{Samples: hopSamples(maximumSamplesPerBatch + 1)})
	if len(batches) != 2 || len(batches[0]) != maximumSamplesPerBatch || len(batches[1]) != 1 {
		t.Fatalf("chunked batches = %d/%d", len(batches[0]), len(batches[1]))
	}
}

func TestParseMTRReportBuildsFixedSamples(t *testing.T) {
	report, err := parseMTR([]byte(`{"report":{"hubs":[
		{"count":1,"host":"192.0.2.1","Loss%":0.0,"Snt":10,"Last":1.1,"Avg":1.2,"Best":1.0,"Wrst":1.5,"Recv":10},
		{"count":2,"host":"203.0.113.5","Loss%":10.0,"Snt":10,"Last":2.2,"Avg":2.3,"Best":2.0,"Wrst":2.8,"Recv":9}]}}`))
	if err != nil {
		t.Fatalf("parseMTR error = %v", err)
	}
	frames := mtrFrames(report, jobSpec{Destination: "203.0.113.5"})
	if len(frames) != 1 || len(frames[0].Hops) != 2 || len(frames[0].Samples) != 14 {
		t.Fatalf("frames = %+v", frames)
	}
	if frames[0].Hops[1].Address != "203.0.113.5" || !frames[0].Hops[1].Reached {
		t.Fatalf("hop identity = %+v", frames[0].Hops[1])
	}
	loss := frames[0].Samples[11]
	if loss.Metric != model.DiagnosticMetricMTRLossPercent || loss.Value != 10.0 || loss.Hop != 2 {
		t.Fatalf("loss sample = %+v", loss)
	}
	for _, sample := range frames[0].Samples {
		if sample.Metric == model.DiagnosticMetricMTRLatencyMS && sample.Stat == "" {
			t.Fatalf("latency sample without stat = %+v", sample)
		}
	}
}

func TestParseMTRReportRejectsMalformedOutput(t *testing.T) {
	cases := map[string]string{
		"empty":            ``,
		"not json":         `nonsense`,
		"no hubs":          `{"report":{"hubs":[]}}`,
		"duplicate hop":    `{"report":{"hubs":[{"count":1,"Snt":1,"Recv":1},{"count":1,"Snt":1,"Recv":1}]}}`,
		"receive overflow": `{"report":{"hubs":[{"count":1,"Snt":1,"Recv":2}]}}`,
		"negative loss":    `{"report":{"hubs":[{"count":1,"Snt":1,"Recv":1,"Last":-1}]}}`,
	}
	for name, output := range cases {
		if _, err := parseMTR([]byte(output)); !errors.Is(err, ErrExecution) {
			t.Fatalf("%s: error = %v, want ErrExecution", name, err)
		}
	}
}

func TestStreamIperf3BuildsIntervalAndSummarySamples(t *testing.T) {
	stream := strings.Join([]string{
		`{"event":"start","data":{"version":"3.16"}}`,
		`{"event":"interval","data":{"streams":[{"sender":true,"bits_per_second":120000000,"bytes":1500000}]}}`,
		`{"event":"end","data":{"sum_sent":{"bits_per_second":110000000,"bytes":1400000,"retransmits":3},` +
			`"sum_received":{"bits_per_second":105000000,"bytes":1300000,"jitter_ms":0.4,"lost_percent":0.5}}}`,
	}, "\n")
	spec := jobSpec{Protocol: model.DiagnosticProtocolTCP, Direction: model.DiagnosticDirectionForward}
	var frames []batch
	summary, err := streamIperf3([]byte(stream), spec, func(frame batch) error {
		frames = append(frames, frame)
		return nil
	})
	if err != nil {
		t.Fatalf("streamIperf3 error = %v", err)
	}
	if len(frames) != 1 || len(frames[0].Samples) != 2 {
		t.Fatalf("interval frames = %+v", frames)
	}
	if frames[0].Samples[0].Metric != model.DiagnosticMetricIperfBytesPerSecond || frames[0].Samples[0].Role != model.DiagnosticRoleSender {
		t.Fatalf("interval sample = %+v", frames[0].Samples[0])
	}
	if final := summary.finalSamples(spec); len(final) != 7 {
		t.Fatalf("final samples = %+v", final)
	}
	if summary.actualBytes() != 2700000 {
		t.Fatalf("actual bytes = %d", summary.actualBytes())
	}
}

func TestStreamIperf3RejectsErrorsAndEmptySummary(t *testing.T) {
	spec := jobSpec{Protocol: model.DiagnosticProtocolTCP, Direction: model.DiagnosticDirectionForward}
	if _, err := streamIperf3([]byte(`{"event":"interval","data":{"bits_per_second":-1}}`), spec, func(batch) error { return nil }); !errors.Is(err, ErrExecution) {
		t.Fatalf("negative throughput error = %v", err)
	}
	if _, err := streamIperf3([]byte(`{"error":"connect failed"}`), spec, func(batch) error { return nil }); !errors.Is(err, ErrExecution) {
		t.Fatalf("error event error = %v", err)
	}
	summary, err := streamIperf3([]byte(`{"event":"start","data":{}}`), spec, func(batch) error { return nil })
	if err != nil || !summary.empty() {
		t.Fatalf("empty summary = %+v (%v)", summary, err)
	}
}

func TestMTRArgumentsUseFixedFlags(t *testing.T) {
	arguments, err := mtrArguments("/usr/bin/mtr", jobSpec{Destination: "2001:db8::1", Protocol: model.DiagnosticProtocolUDP, Probes: 5, HopLimit: 8})
	if err != nil {
		t.Fatalf("mtrArguments error = %v", err)
	}
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "-j -c 5 -m 8 -6 -u -- 2001:db8::1") {
		t.Fatalf("arguments = %q", joined)
	}
	if _, err := mtrArguments("bin/mtr", jobSpec{Destination: "8.8.8.8"}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("relative binary error = %v", err)
	}
	if _, err := mtrArguments("/usr/bin/mtr", jobSpec{Destination: "8.8.8.8", Protocol: "icmpv9"}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("unknown protocol error = %v", err)
	}
}

func TestIperf3ArgumentsUseFixedFlags(t *testing.T) {
	arguments, err := iperf3Arguments("/usr/bin/iperf3", jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolTCP,
		Direction: model.DiagnosticDirectionReverse, Port: 5201, DurationSeconds: 5, ParallelStreams: 2, BitrateMbps: 50})
	if err != nil {
		t.Fatalf("iperf3Arguments error = %v", err)
	}
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "-c 8.8.8.8 -p 5201 -t 5 -P 2 -b 50M -J --json-stream -R") {
		t.Fatalf("arguments = %q", joined)
	}
	if _, err := iperf3Arguments("/usr/bin/iperf3", jobSpec{Destination: "8.8.8.8", Port: 0}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("port error = %v", err)
	}
}

func TestRunFixedCommandExecutesAbsoluteBinaryOnly(t *testing.T) {
	directory := t.TempDir()
	output, err := runFixedCommand(context.Background(), commandSpec{path: "/bin/echo", arguments: []string{"hello"},
		directory: directory, maximumBytes: 1024})
	if err != nil {
		t.Skipf("process execution unavailable in this environment: %v", err)
	}
	if strings.TrimSpace(string(output)) != "hello" {
		t.Fatalf("output = %q", output)
	}
	if _, err := runFixedCommand(context.Background(), commandSpec{path: "echo", directory: directory, maximumBytes: 16}); err == nil {
		t.Fatal("relative path must be rejected")
	}
	outcome, err := classifyRunError(context.Background(), errors.New("boom"))
	if err != nil || outcome.State != model.DiagnosticStateFailed || outcome.FailureCode != "internal" {
		t.Fatalf("classification = %+v (%v)", outcome, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, _ = classifyRunError(cancelled, errors.New("boom"))
	if outcome.State != model.DiagnosticStateCancelled {
		t.Fatalf("cancelled classification = %+v", outcome)
	}
}

func TestBoundedBufferTruncatesOutput(t *testing.T) {
	buffer := boundedBuffer{maximum: 4}
	written, err := buffer.Write([]byte("abcdefgh"))
	if err != nil || written != 8 || !buffer.truncated || string(buffer.Bytes()) != "abcd" {
		t.Fatalf("bounded buffer = %q truncated=%v (%v)", buffer.Bytes(), buffer.truncated, err)
	}
	if _, err := buffer.Write([]byte("x")); err != nil {
		t.Fatalf("write error = %v", err)
	}
}

func TestPrivateStateDirectoryEnforcesPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := ensurePrivateDir(directory); err != nil {
		t.Fatalf("ensurePrivateDir error = %v", err)
	}
	target := filepath.Join(directory, "entry.json")
	if err := writePrivateFile(target, []byte("{}")); err != nil {
		t.Fatalf("writePrivateFile error = %v", err)
	}
	content, err := readPrivateFile(target)
	if err != nil || string(content) != "{}" {
		t.Fatalf("readPrivateFile = %q (%v)", content, err)
	}
	if err := ensurePrivateDir("relative"); !errors.Is(err, ErrStorage) {
		t.Fatalf("relative directory error = %v", err)
	}
	if err := removePrivateFile(target); err != nil {
		t.Fatalf("removePrivateFile error = %v", err)
	}
	if err := removePrivateFile(target); err != nil {
		t.Fatalf("second removePrivateFile error = %v", err)
	}
	content, err = readPrivateFile(target)
	if err != nil || content != nil {
		t.Fatalf("missing file = %q (%v)", content, err)
	}
}
