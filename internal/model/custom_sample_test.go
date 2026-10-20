package model

import (
	"maps"
	"strings"
	"testing"
	"time"
)

func TestCustomSourceMetadataRequiresExactKeysAndBoundedExecutionIdentity(t *testing.T) {
	report := validAgentReport()
	for _, change := range []func(*CustomSampleMetadata){
		func(s *CustomSampleMetadata) { s.SampleID = "invalid" }, func(s *CustomSampleMetadata) { s.DefinitionVersion = "\xff" },
		func(s *CustomSampleMetadata) { s.CadenceMS = 1 }, func(s *CustomSampleMetadata) { s.CadenceMS = MaximumAgentReportInterval.Milliseconds() + 1 },
		func(s *CustomSampleMetadata) { s.CapturedAt = report.CapturedAt.Add(time.Nanosecond) },
		func(s *CustomSampleMetadata) {
			s.CapturedAt = report.CapturedAt.Add(-MaximumCustomSampleAge - time.Nanosecond)
		},
	} {
		metadata := report.CustomSamples["queue"]
		change(&metadata)
		sources := map[string]CustomSampleMetadata{"queue": metadata}
		if err := ValidateCustomSamples(report.CustomMetrics, sources, report.CapturedAt); err == nil {
			t.Fatal("invalid actual-source identity accepted")
		}
	}
	if err := ValidateCustomSamples(report.CustomMetrics, nil, report.CapturedAt); err == nil {
		t.Fatal("missing provenance accepted")
	}
	if err := ValidateCustomSamples(report.CustomMetrics, map[string]CustomSampleMetadata{"other": report.CustomSamples["queue"]}, report.CapturedAt); err == nil {
		t.Fatal("different provenance key accepted")
	}
	encoded, err := EncodeCustomSampleMetadata(report.CustomSamples)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCustomSampleMetadata(encoded, report.CapturedAt)
	if err != nil || decoded["queue"] != report.CustomSamples["queue"] {
		t.Fatal("metadata roundtrip changed source", err)
	}
	if _, err := DecodeCustomSampleMetadata(strings.Repeat(" ", MaximumCustomSourcesBytes+1), report.CapturedAt); err == nil {
		t.Fatal("unbounded metadata admitted")
	}
	if _, err := DecodeCustomSampleMetadata(encoded+" {}", report.CapturedAt); err == nil {
		t.Fatal("ambiguous metadata admitted")
	}
}

func TestReportDigestBindsCustomSourceWithoutMutatingItsCaller(t *testing.T) {
	report := validAgentReport()
	metadata := report.CustomSamples["queue"]
	metadata.CapturedAt = metadata.CapturedAt.In(time.FixedZone("source", 3600))
	report.CustomSamples = map[string]CustomSampleMetadata{"queue": metadata}
	original := maps.Clone(report.CustomSamples)
	first, err := report.Digest()
	if err != nil || report.CustomSamples["queue"] != original["queue"] {
		t.Fatal("digest mutated pending metadata", err)
	}
	metadata.SampleID = "a6d7a562-05aa-440b-a9e0-3bddfba76dcf"
	report.CustomSamples["queue"] = metadata
	second, err := report.Digest()
	if err != nil || first == second {
		t.Fatal("custom execution was not bound to report digest", err)
	}
}

func TestCustomDefinitionVersionIncludesActualExecutableAndRevision(t *testing.T) {
	definition := CustomMetric{ID: "definition", Name: "queue", ShellCommand: "printf 7", IntervalSeconds: 5, UpdatedAt: time.Now().UTC()}
	base := CustomMetricDefinitionVersion(definition)
	for _, change := range []func(*CustomMetric){func(m *CustomMetric) { m.ShellCommand = "printf 8" }, func(m *CustomMetric) { m.IntervalSeconds = 10 }, func(m *CustomMetric) { m.UpdatedAt = m.UpdatedAt.Add(time.Nanosecond) }} {
		next := definition
		change(&next)
		if base == CustomMetricDefinitionVersion(next) {
			t.Fatal("definition change reused execution version")
		}
	}
}
