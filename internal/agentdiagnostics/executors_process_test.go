package agentdiagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// writeFixtureBinary 写出一个 0700 的固定脚本；它只能被绝对路径调用。
func writeFixtureBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture-binary")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod fixture binary: %v", err)
	}
	return path
}

func collectBatches(t *testing.T) (sink, *[]batch) {
	t.Helper()
	frames := &[]batch{}
	return func(frame batch) error {
		*frames = append(*frames, frame)
		return nil
	}, frames
}

func TestMTRExecutorRunsApprovedBinaryWithFixedArguments(t *testing.T) {
	directory := t.TempDir()
	report := `{"report":{"hubs":[{"count":1,"host":"192.0.2.1","Loss%":0,"Snt":10,"Last":1,"Avg":1,"Best":1,"Wrst":1,"Recv":10}]}}`
	script := writeFixtureBinary(t, "#!/bin/sh\nfor argument in \"$@\"; do printf '%s ' \"$argument\"; done > \""+
		directory+"/arguments\"\nprintf '%s' '"+report+"'\n")
	executor := mtrExecutor{path: script}
	sink, frames := collectBatches(t)
	result, err := executor.Execute(context.Background(), jobSpec{Destination: "203.0.113.9", Protocol: model.DiagnosticProtocolTCP,
		Probes: 10, HopLimit: 32, Directory: directory, IPFamily: model.IPFamilyIPv4}, sink)
	if err != nil || result.State != model.DiagnosticStateSucceeded || executor.Kind() != model.DiagnosticKindMTR {
		t.Fatalf("execute = %+v (%v)", result, err)
	}
	if len(*frames) != 1 || len((*frames)[0].Samples) != 7 || len((*frames)[0].Hops) != 1 {
		t.Fatalf("frames = %+v", *frames)
	}
	arguments, err := os.ReadFile(filepath.Join(directory, "arguments"))
	if err != nil {
		t.Fatalf("read arguments: %v", err)
	}
	if !strings.Contains(string(arguments), "-j -c 10 -m 32 -4 -T -- 203.0.113.9") {
		t.Fatalf("arguments = %q", arguments)
	}
}

func TestMTRExecutorReportsFixedFailureCodes(t *testing.T) {
	directory := t.TempDir()
	cases := map[string]struct {
		body    string
		state   string
		failure string
	}{
		"exit_nonzero": {"#!/bin/sh\nexit 3\n", model.DiagnosticStateFailed, "exit_nonzero"},
		"parse_failed": {"#!/bin/sh\nprintf 'not json'\n", model.DiagnosticStateFailed, "parse_failed"},
		"partial": {"#!/bin/sh\nprintf '%s' '{\"report\":{\"hubs\":[{\"count\":1,\"Snt\":10,\"Recv\":0}]}}'\n",
			model.DiagnosticStatePartial, ""},
	}
	for name, testCase := range cases {
		executor := mtrExecutor{path: writeFixtureBinary(t, testCase.body)}
		sink, _ := collectBatches(t)
		result, err := executor.Execute(context.Background(), jobSpec{Destination: "8.8.8.8",
			Protocol: model.DiagnosticProtocolICMP, Probes: 10, HopLimit: 32, Directory: directory}, sink)
		if err != nil || result.State != testCase.state || result.FailureCode != testCase.failure {
			t.Fatalf("%s: execute = %+v (%v)", name, result, err)
		}
	}
}

func TestIperf3ExecutorStreamsAndSums(t *testing.T) {
	directory := t.TempDir()
	stream := `{"event":"start","data":{}}
{"event":"interval","data":{"streams":[{"sender":true,"bits_per_second":120000000,"bytes":1500000}]}}
{"event":"end","data":{"sum_sent":{"bits_per_second":110000000,"bytes":1400000},"sum_received":{"bits_per_second":105000000,"bytes":1300000}}}`
	script := writeFixtureBinary(t, "#!/bin/sh\nfor argument in \"$@\"; do printf '%s ' \"$argument\"; done > \""+
		directory+"/arguments\"\ncat <<'PAYLOAD'\n"+stream+"\nPAYLOAD\n")
	executor := iperf3Executor{path: script}
	sink, frames := collectBatches(t)
	result, err := executor.Execute(context.Background(), jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolTCP,
		Direction: model.DiagnosticDirectionForward, Port: 5201, DurationSeconds: 5, ParallelStreams: 1, BitrateMbps: 20,
		Directory: directory, IPFamily: model.IPFamilyIPv4}, sink)
	if err != nil || result.State != model.DiagnosticStateSucceeded || result.ActualBytes != 2700000 {
		t.Fatalf("execute = %+v (%v)", result, err)
	}
	if len(*frames) != 2 || len((*frames)[1].Samples) != 4 {
		t.Fatalf("frames = %+v", *frames)
	}
	arguments, err := os.ReadFile(filepath.Join(directory, "arguments"))
	if err != nil {
		t.Fatalf("read arguments: %v", err)
	}
	if !strings.Contains(string(arguments), "-c 8.8.8.8 -p 5201 -t 5 -P 1 -b 20M -J --json-stream") {
		t.Fatalf("arguments = %q", arguments)
	}
	if executor.Kind() != model.DiagnosticKindIperf3 {
		t.Fatalf("kind = %q", executor.Kind())
	}
}

func TestIperf3ExecutorReportsMissingSummary(t *testing.T) {
	executor := iperf3Executor{path: writeFixtureBinary(t, "#!/bin/sh\nprintf '%s' '{\"event\":\"start\",\"data\":{}}'\n")}
	sink, _ := collectBatches(t)
	result, err := executor.Execute(context.Background(), jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolTCP,
		Port: 5201, DurationSeconds: 3, ParallelStreams: 1, BitrateMbps: 1, Directory: t.TempDir()}, sink)
	if err != nil || result.State != model.DiagnosticStatePartial || result.FailureCode != "missing_summary" {
		t.Fatalf("execute = %+v (%v)", result, err)
	}
}

func TestExecutorCancellationTerminatesTheProcessTree(t *testing.T) {
	executor := mtrExecutor{path: writeFixtureBinary(t, "#!/bin/sh\nsleep 30\n")}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sink, _ := collectBatches(t)
	started := time.Now()
	result, err := executor.Execute(ctx, jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolICMP,
		Probes: 10, HopLimit: 32, Directory: t.TempDir()}, sink)
	if err != nil || result.State != model.DiagnosticStateCancelled {
		t.Fatalf("execute = %+v (%v)", result, err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestRunFixedCommandClassifiesFailures(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing-binary")
	if _, err := runFixedCommand(context.Background(), commandSpec{path: missing, directory: directory, maximumBytes: 16}); err == nil {
		t.Fatal("missing binary must fail")
	} else {
		outcome, classifyErr := classifyRunError(context.Background(), err)
		if classifyErr != nil || outcome.FailureCode != "start_failed" {
			t.Fatalf("missing binary outcome = %+v (%v)", outcome, classifyErr)
		}
	}
	noisy := writeFixtureBinary(t, "#!/bin/sh\nprintf '0123456789'\n")
	if _, err := runFixedCommand(context.Background(), commandSpec{path: noisy, directory: directory, maximumBytes: 4}); err == nil {
		t.Fatal("oversized output must fail")
	} else {
		outcome, _ := classifyRunError(context.Background(), err)
		if outcome.State != model.DiagnosticStateFailed || outcome.FailureCode != "output_limit" {
			t.Fatalf("oversized outcome = %+v", outcome)
		}
	}
	var failure runError
	if !errors.As(runError{kind: "timeout"}, &failure) || failure.Error() != "timeout" {
		t.Fatalf("runError = %+v", failure)
	}
	if outcome, _ := classifyRunError(context.Background(), runError{kind: "timeout"}); outcome.State != model.DiagnosticStateTimedOut {
		t.Fatalf("timeout outcome = %+v", outcome)
	}
	if outcome, _ := classifyRunError(context.Background(), runError{kind: "unsupported"}); outcome.FailureCode != "unsupported_platform" {
		t.Fatalf("unsupported outcome = %+v", outcome)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	// 调用方取消优先于内部的 deadline 分类：两者都是 cancelled。
	if outcome, _ := classifyRunError(expired, errors.New("boom")); outcome.State != model.DiagnosticStateCancelled {
		t.Fatalf("deadline outcome = %+v", outcome)
	}
	if outcome, _ := classifyRunError(context.Background(), context.DeadlineExceeded); outcome.State != model.DiagnosticStateTimedOut {
		t.Fatalf("deadline error outcome = %+v", outcome)
	}
}
