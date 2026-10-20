package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestCustomMetricDefinitionValidationPreservesCurrentRegistry(t *testing.T) {
	registry := NewCustomMetricRegistry()
	valid := activeCustomMetric("healthy")
	replaceCustomRegistry(t, registry, []model.CustomMetric{valid})
	for name, mutate := range map[string]func(*model.CustomMetric){
		"empty name":    func(m *model.CustomMetric) { m.Name = "" },
		"long name":     func(m *model.CustomMetric) { m.Name = strings.Repeat("a", 101) },
		"invalid name":  func(m *model.CustomMetric) { m.Name = "bad name" },
		"empty command": func(m *model.CustomMetric) { m.ShellCommand = " \t" },
		"long command":  func(m *model.CustomMetric) { m.ShellCommand = strings.Repeat("a", 2049) },
		"nul command":   func(m *model.CustomMetric) { m.ShellCommand = "printf\x001" },
		"long interval": func(m *model.CustomMetric) { m.IntervalSeconds = 86401 },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if err := registry.Replace([]model.CustomMetric{invalid}); err == nil {
				t.Fatal("invalid definition accepted")
			}
			if len(registry.All()) != 1 || registry.All()[0].Name != valid.Name {
				t.Fatal("invalid registration destroyed current state")
			}
		})
	}
	disabled := valid
	disabled.Enabled = false
	replaceCustomRegistry(t, registry, []model.CustomMetric{disabled})
	if len(registry.All()) != 0 {
		t.Fatal("disabled definition remained executable")
	}
}

func customRegistrationTask(t *testing.T, metric model.CustomMetric, revision time.Time) model.AgentTask {
	t.Helper()
	payload, err := model.EncodeCustomMetricRegisterPayload([]model.CustomMetric{metric})
	if err != nil {
		t.Fatal(err)
	}
	return model.AgentTask{ID: "registration", Type: model.AgentTaskCustomMetricRegister, Payload: payload, CreatedAt: revision}
}

func TestCustomMetricRegistrationIgnoresOlderAndDuplicateTasks(t *testing.T) {
	registry := NewCustomMetricRegistry()
	executor := NewMultiTaskExecutor(nil, registry)
	metric := activeCustomMetric("current")
	now := time.Now().UTC()
	latest := customRegistrationTask(t, metric, now)
	if result := executor.ExecuteTask(t.Context(), latest); !result.Success {
		t.Fatal(result.Error)
	}
	for _, revision := range []time.Time{now.Add(-time.Second), now} {
		metric.Name = "obsolete"
		if result := executor.ExecuteTask(t.Context(), customRegistrationTask(t, metric, revision)); !result.Success {
			t.Fatal(result.Error)
		}
		if registry.All()[0].Name != "current" {
			t.Fatal("stale registration replaced current snapshot")
		}
	}
	metric.Name = "next"
	if result := executor.ExecuteTask(t.Context(), customRegistrationTask(t, metric, now.Add(time.Second))); !result.Success {
		t.Fatal(result.Error)
	}
	if registry.All()[0].Name != "next" {
		t.Fatal("newer registration was ignored")
	}
}

func TestCustomMetricRegistrationRejectsUnavailableOversizedAndInvalidPayload(t *testing.T) {
	metric := activeCustomMetric("valid")
	task := customRegistrationTask(t, metric, time.Now())
	if result := NewMultiTaskExecutor(nil, nil).ExecuteTask(t.Context(), task); result.Success {
		t.Fatal("missing registry accepted registration")
	}
	if result := NewMultiTaskExecutor(nil, nil).ExecuteTask(t.Context(), model.AgentTask{Type: model.AgentTaskAuthorizedKey}); result.Success {
		t.Fatal("missing authorized key executor accepted task")
	}
	registry := NewCustomMetricRegistry()
	executor := NewMultiTaskExecutor(nil, registry)
	for _, payload := range []string{strings.Repeat(" ", maxCustomMetricRegisterPayload+1), "invalid", `{"metrics":[{"name":"invalid","enabled":true}]}`} {
		task.Payload = payload
		if result := executor.ExecuteTask(t.Context(), task); result.Success {
			t.Fatal("invalid snapshot acknowledged as successful")
		}
		if len(registry.All()) != 0 {
			t.Fatal("invalid snapshot became executable")
		}
	}
}

func TestCustomMetricSampleFiltersPerDefinitionVersionsAndClockRollback(t *testing.T) {
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("retained"), activeCustomMetric("changed")})
	collector := NewCustomMetricCollector(registry)
	now := time.Now()
	collector.now = func() time.Time { return now }
	for _, metric := range registry.All() {
		collector.samples[metric.Name] = customMetricSample{value: 3, version: metric.version, completedAt: now}
	}
	changed := activeCustomMetric("changed")
	changed.ShellCommand = "printf 2"
	replaceCustomRegistry(t, registry, []model.CustomMetric{activeCustomMetric("retained"), changed})
	values := collector.Sample(t.Context())
	if len(values) != 1 || values["retained"] != 3 {
		t.Fatalf("per-definition cache invalidation = %v", values)
	}
	collector.samples["retained"] = customMetricSample{value: 4, version: registry.All()[0].version, completedAt: now.Add(time.Second)}
	if len(collector.Sample(t.Context())) != 0 {
		t.Fatal("future sample accepted after clock regression")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if collector.Sample(ctx) != nil {
		t.Fatal("canceled report produced samples")
	}
	if collector := NewCustomMetricCollector(nil); len(collector.Sample(t.Context())) != 0 {
		t.Fatal("empty collector produced samples")
	}
}
