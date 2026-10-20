package model

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func validAgentReport() AgentReport {
	return AgentReport{ProtocolVersion: AgentReportProtocol, SampleID: "d6d7a562-05aa-440b-a9e0-3bddfba76dce", CapturedAt: time.Unix(1_700_000_000, 123).UTC(),
		ReportIntervalMS: 5000, Name: "node", Host: "192.0.2.1", Port: 22, Metrics: &NodeMetrics{CPU: 25}, CustomMetrics: map[string]float64{"queue": 7}, CustomSamples: map[string]CustomSampleMetadata{"queue": {SampleID: "d6d7a562-05aa-440b-a9e0-3bddfba76dcf", CapturedAt: time.Unix(1_700_000_000, 123).UTC(), CadenceMS: 5000, DefinitionVersion: strings.Repeat("a", 64)}}}
}

func TestAgentReportValidationBoundsProtocolIdentityTimeAndValues(t *testing.T) {
	valid := validAgentReport()
	if err := valid.Validate(valid.CapturedAt); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AgentReport){
		"old protocol": func(r *AgentReport) { r.ProtocolVersion = 1 }, "bad sample ID": func(r *AgentReport) { r.SampleID = "not-uuid" },
		"future clock":    func(r *AgentReport) { r.CapturedAt = r.CapturedAt.Add(MaximumAgentClockSkew + time.Second) },
		"expired":         func(r *AgentReport) { r.CapturedAt = r.CapturedAt.Add(-MaximumAgentSampleAge - time.Second) },
		"missing metrics": func(r *AgentReport) { r.Metrics = nil }, "invalid metrics": func(r *AgentReport) { r.Metrics = &NodeMetrics{CPU: 101} },
		"invalid interval": func(r *AgentReport) { r.ReportIntervalMS = 0 }, "long host": func(r *AgentReport) { r.Host = strings.Repeat("x", MaximumAgentHostBytes+1) },
		"invalid custom": func(r *AgentReport) { r.CustomMetrics = map[string]float64{"bad name": 1} }, "nonfinite": func(r *AgentReport) { r.CustomMetrics = map[string]float64{"queue": math.NaN()} },
	} {
		t.Run(name, func(t *testing.T) {
			report := valid
			mutate(&report)
			if err := report.Validate(valid.CapturedAt); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
	old := valid
	old.ProtocolVersion = 1
	if err := old.Validate(valid.CapturedAt); !errors.Is(err, ErrAgentReportProtocol) {
		t.Fatal("old protocol not explicitly rejected")
	}
}

func TestAgentReportDigestIsStableAndIncludesCapturedPayload(t *testing.T) {
	first := validAgentReport()
	second := validAgentReport()
	first.CustomMetrics = map[string]float64{"a": 1, "b": 2}
	second.CustomMetrics = map[string]float64{"b": 2, "a": 1}
	one, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Digest()
	if err != nil || one != two {
		t.Fatal("map order changed payload identity")
	}
	second.CapturedAt = second.CapturedAt.Add(time.Nanosecond)
	other, err := second.Digest()
	if err != nil || other == one {
		t.Fatal("capture time was not bound to receipt")
	}
	second = first
	second.Host = "192.0.2.2"
	other, err = second.Digest()
	if err != nil || other == one {
		t.Fatal("inventory was not bound to receipt")
	}
}

func TestTelemetryFreshnessSeparatesContactAndCapturedTime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, test := range []struct {
		age   time.Duration
		state string
	}{{-time.Second, TelemetryFresh}, {time.Second, TelemetryFresh}, {40 * time.Second, TelemetryDelayed}, {2 * time.Minute, TelemetryStale}} {
		at := now.Add(-test.age)
		freshness := NewTelemetryFreshness(now, &at, 5*time.Second)
		if freshness.TelemetryState != test.state || freshness.TelemetryAgeSeconds == nil || *freshness.TelemetryAgeSeconds < 0 {
			t.Fatalf("freshness=%+v", freshness)
		}
	}
	if result := NewTelemetryFreshness(now, nil, 5*time.Second); result.TelemetryState != TelemetryUnavailable || result.TelemetryAt != nil {
		t.Fatal("missing receipt invented freshness")
	}
}
