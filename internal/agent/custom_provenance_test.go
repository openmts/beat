package agent

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestCustomCollectorPreservesRealCompletionIdentityAndDefinition(t *testing.T) {
	registry := NewCustomMetricRegistry()
	definition := model.CustomMetric{ID: "definition", Name: "queue", ShellCommand: "printf 7", IntervalSeconds: 5, Enabled: true, UpdatedAt: time.Now().UTC()}
	replaceCustomRegistry(t, registry, []model.CustomMetric{definition})
	collector := NewCustomMetricCollector(registry)
	collector.execute = func(context.Context, registeredMetric) (float64, error) { return 7, nil }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 1 })
	first := collector.SampleProvenance(t.Context())
	source := first.Samples["queue"]
	if first.Values["queue"] != 7 || source.SampleID == "" || source.CapturedAt.IsZero() || source.CadenceMS != 5000 || source.DefinitionVersion != model.CustomMetricDefinitionVersion(definition) {
		t.Fatal("collector lost real provenance")
	}
	if second := collector.SampleProvenance(t.Context()); len(second.Values) != 0 || len(second.Samples) != 0 {
		t.Fatal("one execution was reported twice")
	}
	definition.UpdatedAt = definition.UpdatedAt.Add(time.Second)
	replaceCustomRegistry(t, registry, []model.CustomMetric{definition})
	waitCustomCondition(t, func() bool { collector.mu.Lock(); defer collector.mu.Unlock(); return len(collector.samples) == 1 })
	second := collector.SampleProvenance(t.Context())
	if second.Samples["queue"].SampleID == source.SampleID || second.Samples["queue"].DefinitionVersion == source.DefinitionVersion {
		t.Fatal("new definition reused prior execution identity")
	}
}

func TestPendingReportClonesBothCustomValuesAndProvenance(t *testing.T) {
	at := time.Now().UTC()
	source := model.CustomSampleMetadata{SampleID: "original", CapturedAt: at}
	custom := model.CollectedCustomSamples{Values: map[string]float64{"queue": 7}, Samples: map[string]model.CustomSampleMetadata{"queue": source}}
	sample, err := newReportSample(model.NodeMetrics{}, custom, at)
	if err != nil {
		t.Fatal(err)
	}
	custom.Values["queue"] = 9
	custom.Samples["queue"] = model.CustomSampleMetadata{SampleID: "newer", CapturedAt: at.Add(time.Second)}
	if sample.CustomMetrics["queue"] != 7 || sample.CustomSamples["queue"] != source {
		t.Fatal("background cache mutated pending data or identity")
	}
	delete(custom.Samples, "queue")
	if sample.CustomSamples["queue"].SampleID != "original" {
		t.Fatal("revocation erased immutable pending identity")
	}
}
