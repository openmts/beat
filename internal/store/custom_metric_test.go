package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func validCustomMetric() model.CustomMetric {
	return model.CustomMetric{
		Name:            "http_latency",
		ShellCommand:    "curl -so /dev/null -w '%{time_total}' https://example.com",
		IntervalSeconds: 30,
		Description:     "HTTP request latency",
		Enabled:         true,
	}
}

func setupCustomMetricTest(t *testing.T) (*CustomMetricStore, *NodeStore) {
	t.Helper()
	s := setupTestDB(t)
	return NewCustomMetricStore(s.DB), NewNodeStore(s.DB)
}

func TestCustomMetricStoreLifecycle(t *testing.T) {
	store, _ := setupCustomMetricTest(t)
	ctx := context.Background()

	created, err := store.Create(ctx, validCustomMetric())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || !created.Enabled {
		t.Fatalf("created = %#v", created)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Name != "http_latency" {
		t.Fatalf("got = %#v", got)
	}

	updated := *created
	updated.Name = "renamed"
	updated.IntervalSeconds = 60
	result, err := store.Update(ctx, created.ID, updated)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result == nil || result.IntervalSeconds != 60 {
		t.Fatalf("updated = %#v", result)
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if after != nil {
		t.Fatalf("metric should be deleted, got %#v", after)
	}
}

func TestCustomMetricStoreValidation(t *testing.T) {
	store, _ := setupCustomMetricTest(t)
	ctx := context.Background()

	invalid := []model.CustomMetric{
		{Name: "", ShellCommand: "echo 1", IntervalSeconds: 30},
		{Name: "ok", ShellCommand: "", IntervalSeconds: 30},
		{Name: "ok", ShellCommand: "echo 1", IntervalSeconds: 2},
		{Name: "ok", ShellCommand: "echo 1", IntervalSeconds: 90000},
	}
	for _, metric := range invalid {
		if _, err := store.Create(ctx, metric); err == nil {
			t.Fatalf("accepted invalid metric %#v", metric)
		}
	}
}

func TestCustomMetricStoreNameTaken(t *testing.T) {
	store, _ := setupCustomMetricTest(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, validCustomMetric()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, validCustomMetric()); err != ErrCustomMetricNameTaken {
		t.Fatalf("error = %v, want ErrCustomMetricNameTaken", err)
	}
}

func TestCustomMetricStoreNodeBindings(t *testing.T) {
	store, nodeStore := setupCustomMetricTest(t)
	ctx := context.Background()
	node, err := nodeStore.UpsertNode(ctx, "node-custom", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	first, err := store.Create(ctx, validCustomMetric())
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := validCustomMetric()
	second.Name = "disk_io"
	second.ShellCommand = "echo 42"
	secondMetric, err := store.Create(ctx, second)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	if err := store.SetNodeBindings(ctx, node.ID, []string{first.ID, secondMetric.ID}); err != nil {
		t.Fatalf("set bindings: %v", err)
	}
	ids, err := store.NodeMetricIDs(ctx, node.ID)
	if err != nil {
		t.Fatalf("node metric ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("node metric ids = %v, want 2", ids)
	}
	enabled, err := store.ListEnabledForNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 2 {
		t.Fatalf("enabled = %v, want 2", enabled)
	}

	// Replace with only the first metric.
	if err := store.SetNodeBindings(ctx, node.ID, []string{first.ID}); err != nil {
		t.Fatalf("replace bindings: %v", err)
	}
	ids, err = store.NodeMetricIDs(ctx, node.ID)
	if err != nil {
		t.Fatalf("node metric ids after replace: %v", err)
	}
	if len(ids) != 1 || ids[0] != first.ID {
		t.Fatalf("node metric ids after replace = %v, want [%s]", ids, first.ID)
	}

	// Disabled metrics are not listed.
	disabled := *secondMetric
	disabled.Enabled = false
	if _, err := store.Update(ctx, secondMetric.ID, disabled); err != nil {
		t.Fatalf("disable metric: %v", err)
	}
	if err := store.SetNodeBindings(ctx, node.ID, []string{first.ID, secondMetric.ID}); err != nil {
		t.Fatalf("re-add bindings: %v", err)
	}
	enabled, err = store.ListEnabledForNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("list enabled after disable: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != first.ID {
		t.Fatalf("enabled after disable = %v, want only first", enabled)
	}
}

func TestCustomMetricStoreUpdateMissing(t *testing.T) {
	store, _ := setupCustomMetricTest(t)
	ctx := context.Background()
	result, err := store.Update(ctx, "missing", validCustomMetric())
	if err != nil {
		t.Fatalf("update missing: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

func TestCustomMetricStoreListAndMetricNodes(t *testing.T) {
	store, nodeStore := setupCustomMetricTest(t)
	ctx := context.Background()
	node, err := nodeStore.UpsertNode(ctx, "node-list", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if _, err := store.Create(ctx, validCustomMetric()); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := validCustomMetric()
	second.Name = "second_metric"
	second.ShellCommand = "echo 2"
	secondMetric, err := store.Create(ctx, second)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %d, want 2", len(list))
	}

	if err := store.SetMetricNodes(ctx, secondMetric.ID, []string{node.ID}); err != nil {
		t.Fatalf("set metric nodes: %v", err)
	}
	ids, err := store.NodeIDsForMetric(ctx, secondMetric.ID)
	if err != nil {
		t.Fatalf("node ids for metric: %v", err)
	}
	if len(ids) != 1 || ids[0] != node.ID {
		t.Fatalf("ids = %v", ids)
	}
	// Replacing clears previous nodes.
	if err := store.SetMetricNodes(ctx, secondMetric.ID, nil); err != nil {
		t.Fatalf("clear metric nodes: %v", err)
	}
	ids, err = store.NodeIDsForMetric(ctx, secondMetric.ID)
	if err != nil {
		t.Fatalf("node ids after clear: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids after clear = %v", ids)
	}
}

func TestCustomMetricStoreDeleteMissingAndUpdateConflict(t *testing.T) {
	store, _ := setupCustomMetricTest(t)
	ctx := context.Background()
	if err := store.Delete(ctx, "missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	first, err := store.Create(ctx, validCustomMetric())
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := validCustomMetric()
	second.Name = "unique_name"
	secondMetric, err := store.Create(ctx, second)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	conflict := *secondMetric
	conflict.Name = first.Name
	if _, err := store.Update(ctx, secondMetric.ID, conflict); err != ErrCustomMetricNameTaken {
		t.Fatalf("error = %v, want name taken", err)
	}
	_ = first
}
