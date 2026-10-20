package model

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestAgentProtocolBoundsMetadataAndClockEdges(t *testing.T) {
	base := validAgentReport()
	for _, offset := range []time.Duration{MaximumAgentClockSkew, -MaximumAgentSampleAge} {
		report := base
		report.CapturedAt = base.CapturedAt.Add(offset)
		report.CustomSamples = map[string]CustomSampleMetadata{"queue": base.CustomSamples["queue"]}
		metadata := report.CustomSamples["queue"]
		metadata.CapturedAt = report.CapturedAt
		report.CustomSamples["queue"] = metadata
		if err := report.Validate(base.CapturedAt); err != nil {
			t.Fatal("inclusive clock boundary rejected", err)
		}
	}
	for _, mutate := range []func(*AgentReport){
		func(r *AgentReport) { r.CapturedAt = time.Time{} }, func(r *AgentReport) { r.SampleID = strings.ToUpper(r.SampleID) },
		func(r *AgentReport) { r.ReportIntervalMS = MaximumAgentReportInterval.Milliseconds() + 1 },
		func(r *AgentReport) { r.Name = strings.Repeat("n", MaximumAgentNameBytes+1) },
		func(r *AgentReport) { r.System.CPUModel = strings.Repeat("m", MaximumAgentInventoryBytes+1) },
		func(r *AgentReport) { r.System.Platform = "bad\nplatform" }, func(r *AgentReport) { r.Host = "\xff" },
		func(r *AgentReport) { r.Port = 0 }, func(r *AgentReport) { r.Host = " " },
		func(r *AgentReport) { r.CustomMetrics = map[string]float64{"": 1} },
		func(r *AgentReport) {
			r.CustomMetrics = map[string]float64{strings.Repeat("x", MaximumAgentCustomNameBytes+1): 1}
		},
		func(r *AgentReport) {
			r.CustomMetrics = make(map[string]float64)
			for index := range MaximumAgentCustomMetrics + 1 {
				r.CustomMetrics[strings.Repeat("a", index+1)] = 1
			}
		},
	} {
		report := base
		mutate(&report)
		if err := report.Validate(base.CapturedAt); err == nil {
			t.Fatal("unsafe protocol metadata accepted")
		}
	}
	base.CustomMetrics = map[string]float64{"queue": math.NaN()}
	if _, err := base.Digest(); err == nil {
		t.Fatal("nonfinite sample acquired canonical identity")
	}
}

func TestAgentCredentialFingerprintAndACKBindTheirOriginalIdentity(t *testing.T) {
	if AgentCredentialFingerprint(nil) != "" {
		t.Fatal("invalid credential created fingerprint")
	}
	first := AgentCredentialFingerprint([]byte(strings.Repeat("a", 32)))
	second := AgentCredentialFingerprint([]byte(strings.Repeat("b", 32)))
	if first == second || len(first) != 64 || first != AgentCredentialFingerprint([]byte(strings.Repeat("a", 32))) {
		t.Fatal("credential generation was not bound")
	}
	report := validAgentReport()
	ack := AgentReportACK{ProtocolVersion: AgentReportProtocol, SampleID: report.SampleID, CapturedAt: report.CapturedAt}
	if !ack.Matches(report) {
		t.Fatal("matching acknowledgement rejected")
	}
	ack.CapturedAt = ack.CapturedAt.Add(time.Nanosecond)
	if ack.Matches(report) {
		t.Fatal("different captured sample acknowledged")
	}
}

func TestAgentArchiveAndOfflineThresholdsHaveSharedBoundedClockPolicy(t *testing.T) {
	for _, interval := range []time.Duration{-time.Hour, 0, time.Second, 5 * time.Second} {
		if AgentOfflineAfter(interval) != 90*time.Second {
			t.Fatal("minimum offline threshold changed")
		}
	}
	if AgentOfflineAfter(time.Hour) != 3*time.Hour || AgentOfflineAfter(100*time.Hour) != 72*time.Hour {
		t.Fatal("long report interval is not bounded")
	}
	now := time.Now().UTC()
	end, err := AgentArchiveEnd(now)
	if err != nil || !end.Equal(now.Add(MaximumAgentClockSkew)) {
		t.Fatal("future accepted sample excluded")
	}
	for _, invalid := range []time.Time{{}, time.Unix(0, 0), time.Unix(0, math.MaxInt64)} {
		if _, err := AgentArchiveEnd(invalid); err == nil {
			t.Fatal("archive timestamp overflow accepted")
		}
	}
}
