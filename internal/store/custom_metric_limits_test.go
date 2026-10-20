package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestCustomMetricValidationUsesOriginalCommandBytes(t *testing.T) {
	_, metrics, _, _ := customRegistrationFixture(t)
	metric := validCustomMetric()
	metric.Name = "padded_command"
	metric.ShellCommand = "printf 1" + strings.Repeat(" ", 2048)
	if _, err := metrics.Create(t.Context(), metric); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatalf("agent-incompatible command persisted: %v", err)
	}
}

func TestCustomMetricNodeBindingRevocationIsDurable(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], []string{metric.ID}); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	pending, err := NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), nodes[0])
	if err != nil || len(pending) != 1 {
		t.Fatalf("binding mutation has no durable snapshot: %v / %d", err, len(pending))
	}
	if !strings.Contains(pending[0].Payload, `"metrics":[]`) {
		t.Fatal("removed binding retained an executable definition")
	}
}

func customMetricDefinitions(t *testing.T, metrics *CustomMetricStore, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for index := range count {
		metric := validCustomMetric()
		metric.Name = fmt.Sprintf("bounded_%02d", index)
		created, err := metrics.Create(t.Context(), metric)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	return ids
}

func TestCustomMetricEnabledDefinitionLimitRollsBackBindings(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	ids := customMetricDefinitions(t, metrics, maxCustomMetricDefinitionsPerNode+1)
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], ids[:maxCustomMetricDefinitionsPerNode]); err != nil {
		t.Fatal(err)
	}
	before, err := NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.SetMetricNodesWithRegistration(t.Context(), ids[maxCustomMetricDefinitionsPerNode], nodes[:1]); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatalf("65th definition error = %v", err)
	}
	bindings, err := metrics.NodeMetricIDs(t.Context(), nodes[0])
	if err != nil || len(bindings) != maxCustomMetricDefinitionsPerNode {
		t.Fatal("over-limit assignment changed bindings")
	}
	after, err := NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), nodes[0])
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatal("failed assignment changed the pending snapshot")
	}
}

func TestCustomMetricSnapshotLimitRollsBackUpdatedDefinition(t *testing.T) {
	_, metrics, metric, nodes := customRegistrationFixture(t)
	ids := customMetricDefinitions(t, metrics, maxCustomMetricDefinitionsPerNode-1)
	ids = append(ids, metric.ID)
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], ids); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[:len(ids)-1] {
		if _, err := metrics.db.Exec("UPDATE custom_metrics SET shell_command=? WHERE id=?", strings.Repeat("&", 680), id); err != nil {
			t.Fatal(err)
		}
	}
	changed := *metric
	changed.ShellCommand = strings.Repeat("&", 2048)
	if _, err := metrics.UpdateWithRegistration(t.Context(), metric.ID, changed); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatalf("oversized JSON snapshot error = %v", err)
	}
	current, err := metrics.Get(t.Context(), metric.ID)
	if err != nil || current.ShellCommand != metric.ShellCommand {
		t.Fatal("oversized snapshot left updated command in storage")
	}
}

func TestCustomMetricTargetAndAffectedNodeLimits(t *testing.T) {
	_, metrics, metric, _ := customRegistrationFixture(t)
	if _, err := metrics.SetMetricNodesWithRegistration(t.Context(), metric.ID, make([]string, maxCustomMetricTargets+1)); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("excess target list accepted")
	}
	previous := make([]string, maxCustomMetricAffectedNodes)
	for index := range previous {
		previous[index] = fmt.Sprintf("node_%d", index)
	}
	if _, err := customMetricAffectedNodes(previous, []string{"extra"}); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("affected-node union was unbounded")
	}
	union, err := customMetricAffectedNodes(previous, previous)
	if err != nil || len(union) != len(previous) {
		t.Fatal("deduplicated boundary was rejected")
	}
}

func TestCustomMetricDisabledDefinitionsDoNotConsumeExecutionLimit(t *testing.T) {
	_, metrics, metric, nodes := customRegistrationFixture(t)
	ids := customMetricDefinitions(t, metrics, maxCustomMetricDefinitionsPerNode)
	disabled := *metric
	disabled.Enabled = false
	if _, err := metrics.UpdateWithRegistration(t.Context(), metric.ID, disabled); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], append(ids, metric.ID)); err != nil {
		t.Fatal(err)
	}
	changed := disabled
	changed.Enabled = true
	if _, err := metrics.UpdateWithRegistration(t.Context(), metric.ID, changed); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("enable bypassed per-node execution limit")
	}
	stored, err := metrics.Get(t.Context(), metric.ID)
	if err != nil || stored.Enabled {
		t.Fatal("failed enable was not rolled back")
	}
}

func TestCustomMetricDefinitionRejectsAgentIncompatibleNames(t *testing.T) {
	for _, name := range []string{"bad/name", " name", "指标", strings.Repeat("a", 101)} {
		metric := model.CustomMetric{Name: name, ShellCommand: "printf 1", IntervalSeconds: 5}
		if err := validateCustomMetric(metric); !errors.Is(err, ErrCustomMetricInvalid) {
			t.Fatalf("incompatible name %q accepted", name)
		}
	}
}
