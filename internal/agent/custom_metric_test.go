package agent

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestParseMetricValue(t *testing.T) {
	for _, test := range []struct {
		output  string
		want    float64
		wantErr bool
	}{
		{"42", 42, false}, {" 42\n", 42, false}, {"3.14", 3.14, false}, {"1.5 kB\n", 1.5, false},
		{"", 0, true}, {"  \n", 0, true}, {"not-a-number", 0, true},
	} {
		value, err := parseMetricValue(test.output)
		if (err != nil) != test.wantErr || err == nil && value != test.want {
			t.Fatalf("parse %q = %v, %v", test.output, value, err)
		}
	}
}

func TestCollectMetric(t *testing.T) {
	value, ok := CollectMetric(t.Context(), registeredMetric{Name: "test", ShellCommand: "echo 42"})
	if !ok || value != 42 {
		t.Fatal("valid metric was not collected")
	}
	for _, command := range []string{"echo not-a-number", "exit 1", ""} {
		if _, ok := CollectMetric(t.Context(), registeredMetric{Name: "bad", ShellCommand: command}); ok {
			t.Fatal("invalid metric accepted")
		}
	}
}

func replaceCustomRegistry(t *testing.T, registry *CustomMetricRegistry, metrics []model.CustomMetric) {
	t.Helper()
	if err := registry.Replace(metrics); err != nil {
		t.Fatal(err)
	}
}

func startCustomCollector(t *testing.T, collector *CustomMetricCollector) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("collector did not stop")
		}
	})
	return ctx
}

func awaitCustomSamples(t *testing.T, collector *CustomMetricCollector, count int) map[string]float64 {
	t.Helper()
	values := make(map[string]float64)
	deadline := time.Now().Add(time.Second)
	for len(values) < count && time.Now().Before(deadline) {
		for name, value := range collector.Sample(t.Context()) {
			values[name] = value
		}
		if len(values) < count {
			time.Sleep(time.Millisecond)
		}
	}
	if len(values) != count {
		t.Fatalf("received %d samples, expected %d", len(values), count)
	}
	return values
}

func TestCustomMetricRegistryReplaceAndAll(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{
		{Name: "a", ShellCommand: "echo 1", IntervalSeconds: 10, Enabled: true},
		{Name: "b", ShellCommand: "echo 2", IntervalSeconds: 30, Enabled: true},
	})
	if all := registry.All(); len(all) != 2 {
		t.Fatalf("definitions = %d", len(all))
	}
	replaceCustomRegistry(t, registry, []model.CustomMetric{{Name: "c", ShellCommand: "echo 3", IntervalSeconds: 60, Enabled: true}})
	all := registry.All()
	if len(all) != 1 || all[0].Name != "c" {
		t.Fatal("replacement retained old definitions")
	}
	all[0].Name = "mutated"
	if registry.All()[0].Name != "c" {
		t.Fatal("registry snapshot exposed mutable state")
	}
}

func TestCustomMetricCollectorSample(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{
		{Name: "a", ShellCommand: "echo 10", IntervalSeconds: 10, Enabled: true},
		{Name: "b", ShellCommand: "echo 20", IntervalSeconds: 30, Enabled: true},
		{Name: "c", ShellCommand: "exit 1", IntervalSeconds: 10, Enabled: true},
	})
	collector := NewCustomMetricCollector(registry)
	startCustomCollector(t, collector)
	values := awaitCustomSamples(t, collector, 2)
	if values["a"] != 10 || values["b"] != 20 {
		t.Fatal("unexpected metric values")
	}
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("completed samples were reported twice")
	}
}
