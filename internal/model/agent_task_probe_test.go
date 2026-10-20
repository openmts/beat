package model

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCustomMetricProbePayloadRoundTrip(t *testing.T) {
	payload, err := EncodeCustomMetricProbePayload("metric-1", "echo 1")
	if err != nil {
		t.Fatalf("encode probe payload: %v", err)
	}
	decoded, err := DecodeCustomMetricProbePayload(payload)
	if err != nil {
		t.Fatalf("decode probe payload: %v", err)
	}
	if decoded.MetricID != "metric-1" || decoded.ShellCommand != "echo 1" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeCustomMetricProbePayloadRejectsInvalid(t *testing.T) {
	oversizedMetric := strings.Repeat("a", 65)
	oversizedCommand := strings.Repeat("a", MaximumAgentTaskProbeCommandBytes+1)
	tests := []struct {
		name    string
		payload string
	}{
		{name: "malformed json", payload: "{"},
		{name: "missing metric", payload: `{"shell_command":"echo 1"}`},
		{name: "missing command", payload: `{"metric_id":"metric-1"}`},
		{name: "blank metric", payload: `{"metric_id":"   ","shell_command":"echo 1"}`},
		{name: "oversized metric", payload: `{"metric_id":"` + oversizedMetric + `","shell_command":"echo 1"}`},
		{name: "oversized command", payload: `{"metric_id":"metric-1","shell_command":"` + oversizedCommand + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCustomMetricProbePayload(test.payload); !errors.Is(err, ErrAgentTaskInvalidPayload) {
				t.Fatalf("decode error = %v, want ErrAgentTaskInvalidPayload", err)
			}
		})
	}
}

func TestEncodeCustomMetricProbePayloadRejectsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		metricID string
		command  string
	}{
		{name: "blank metric", metricID: "  ", command: "echo 1"},
		{name: "oversized metric", metricID: strings.Repeat("a", 65), command: "echo 1"},
		{name: "blank command", metricID: "metric-1", command: "\t"},
		{name: "oversized command", metricID: "metric-1", command: strings.Repeat("a", MaximumAgentTaskProbeCommandBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := EncodeCustomMetricProbePayload(test.metricID, test.command); !errors.Is(err, ErrAgentTaskInvalidPayload) {
				t.Fatalf("encode error = %v, want ErrAgentTaskInvalidPayload", err)
			}
		})
	}
}

func TestTruncateAgentTaskTextKeepsShortValues(t *testing.T) {
	if got := TruncateAgentTaskOutput("short output"); got != "short output" {
		t.Fatalf("short output = %q", got)
	}
	if got := TruncateAgentTaskFailure("short failure"); got != "short failure" {
		t.Fatalf("short failure = %q", got)
	}
}

func TestTruncateAgentTaskTextBoundsLongValues(t *testing.T) {
	output := TruncateAgentTaskOutput(strings.Repeat("x", MaximumAgentTaskOutputBytes+50))
	if len(output) != MaximumAgentTaskOutputBytes {
		t.Fatalf("output length = %d, want %d", len(output), MaximumAgentTaskOutputBytes)
	}
	failure := TruncateAgentTaskFailure(strings.Repeat("y", MaximumAgentTaskFailureBytes+50))
	if len(failure) != MaximumAgentTaskFailureBytes {
		t.Fatalf("failure length = %d, want %d", len(failure), MaximumAgentTaskFailureBytes)
	}
}

func TestTruncateAgentTaskOutputKeepsUTF8Boundary(t *testing.T) {
	value := strings.Repeat("\u6d4b", MaximumAgentTaskOutputBytes)
	truncated := TruncateAgentTaskOutput(value)
	if !utf8.ValidString(truncated) {
		t.Fatal("truncated output is not valid UTF-8")
	}
	// 三字节字符被边界截断时必须回退到合法边界，而不是留下半个字符。
	want := MaximumAgentTaskOutputBytes - MaximumAgentTaskOutputBytes%3
	if len(truncated) != want {
		t.Fatalf("truncated length = %d, want %d", len(truncated), want)
	}
}
