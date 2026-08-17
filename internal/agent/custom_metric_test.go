package agent

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestParseMetricValue(t *testing.T) {
	tests := []struct {
		output  string
		want    float64
		wantErr bool
	}{
		{output: "42", want: 42},
		{output: " 42\n", want: 42},
		{output: "3.14", want: 3.14},
		{output: "1.5 kB\n", want: 1.5},
		{output: "", wantErr: true},
		{output: "  \n", wantErr: true},
		{output: "not-a-number", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseMetricValue(tt.output)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseMetricValue(%q) error = %v, wantErr %v", tt.output, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Fatalf("parseMetricValue(%q) = %v, want %v", tt.output, got, tt.want)
		}
	}
}

func TestCollectMetric(t *testing.T) {
	ctx := context.Background()
	value, ok := CollectMetric(ctx, registeredMetric{Name: "test", ShellCommand: "echo 42"})
	if !ok || value != 42 {
		t.Fatalf("value = %v, ok = %v, want 42 true", value, ok)
	}
	if _, ok := CollectMetric(ctx, registeredMetric{Name: "bad", ShellCommand: "echo not-a-number"}); ok {
		t.Fatal("non-numeric output should not be collected")
	}
	if _, ok := CollectMetric(ctx, registeredMetric{Name: "fail", ShellCommand: "exit 1"}); ok {
		t.Fatal("failing command should not be collected")
	}
	if _, ok := CollectMetric(ctx, registeredMetric{Name: "none", ShellCommand: ""}); ok {
		t.Fatal("empty command should not be collected")
	}
}

func TestCustomMetricRegistryReplaceAndAll(t *testing.T) {
	registry := NewCustomMetricRegistry()
	registry.Replace([]model.CustomMetric{
		{Name: "a", ShellCommand: "echo 1", IntervalSeconds: 10},
		{Name: "b", ShellCommand: "echo 2", IntervalSeconds: 30},
	})
	all := registry.All()
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2", len(all))
	}
	// Replace clears the old set.
	registry.Replace([]model.CustomMetric{
		{Name: "c", ShellCommand: "echo 3", IntervalSeconds: 60},
	})
	all = registry.All()
	if len(all) != 1 || all[0].Name != "c" {
		t.Fatalf("all after replace = %#v, want only c", all)
	}
}

func TestCustomMetricCollectorSample(t *testing.T) {
	registry := NewCustomMetricRegistry()
	registry.Replace([]model.CustomMetric{
		{Name: "a", ShellCommand: "echo 10", IntervalSeconds: 10},
		{Name: "b", ShellCommand: "echo 20", IntervalSeconds: 30},
		{Name: "c", ShellCommand: "exit 1", IntervalSeconds: 10},
	})
	collector := NewCustomMetricCollector(registry)
	// Advance time to a multiple of both intervals.
	collector.now = func() time.Time { return time.Unix(60, 0) }
	results := collector.Sample(context.Background())
	if len(results) != 2 {
		t.Fatalf("results = %#v, want 2 (a and b)", results)
	}
	if results["a"] != 10 || results["b"] != 20 {
		t.Fatalf("results = %#v", results)
	}
	if _, ok := results["c"]; ok {
		t.Fatal("failing metric should not be reported")
	}
}

func TestCustomMetricCollectorInterval(t *testing.T) {
	registry := NewCustomMetricRegistry()
	registry.Replace([]model.CustomMetric{
		{Name: "a", ShellCommand: "echo 1", IntervalSeconds: 30},
	})
	collector := NewCustomMetricCollector(registry)
	collector.now = func() time.Time { return time.Unix(61, 0) }
	results := collector.Sample(context.Background())
	if len(results) != 1 || results["a"] != 1 {
		t.Fatalf("results = %#v, want a=1 on first sample", results)
	}
	// Second sample within the interval should be skipped.
	collector.now = func() time.Time { return time.Unix(70, 0) }
	results = collector.Sample(context.Background())
	if len(results) != 0 {
		t.Fatalf("results = %#v, want none before interval elapses", results)
	}
	// After the interval elapses, sample again.
	collector.now = func() time.Time { return time.Unix(91, 0) }
	results = collector.Sample(context.Background())
	if len(results) != 1 || results["a"] != 1 {
		t.Fatalf("results = %#v, want a=1 after interval", results)
	}
}
