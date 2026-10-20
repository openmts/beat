package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func TestAgentPolicyReadsCustomBindingAndBuiltinFromOneTransaction(t *testing.T) {
	sqlite := setupTestDB(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "policy-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	customs := NewCustomMetricStore(sqlite.DB)
	metric, err := customs.Create(t.Context(), model.CustomMetric{Name: "queue", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := customs.SetMetricNodes(t.Context(), metric.ID, []string{node.ID}); err != nil {
		t.Fatal(err)
	}
	catalog := NewMetricCatalogStore(sqlite.DB)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- toggleAgentPolicies(ctx, catalog) }()
	for range 50 {
		policy, err := catalog.PrepareAgentPolicy(ctx, AgentMetricPolicyInput{NodeID: node.ID, Values: map[string]float64{"queue": 7}, Samples: map[string]model.CustomSampleMetadata{"queue": testutil.CustomSourceFixture(time.Now(), *metric)}})
		if err != nil {
			cancel()
			<-finished
			t.Fatal(err)
		}
		if policy.EnabledBuiltins["cpu"] != (len(policy.Custom) == 1) {
			cancel()
			<-finished
			t.Fatal("policy mixed different committed revisions")
		}
	}
	cancel()
	if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func toggleAgentPolicies(ctx context.Context, catalog *MetricCatalogStore) error {
	for enabled := false; ctx.Err() == nil; enabled = !enabled {
		if err := setAgentPolicyPair(ctx, catalog, enabled); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func setAgentPolicyPair(ctx context.Context, catalog *MetricCatalogStore, enabled bool) error {
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "UPDATE metric_definitions SET enabled=? WHERE metric_key IN ('cpu','custom_queue')", enabled); err != nil {
		return err
	}
	return tx.Commit()
}

func TestAgentPolicyPropagatesInvalidBindingAndStorageFailures(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	for _, values := range []map[string]float64{{"queue": math.NaN()}, {"unbound": 1}} {
		if _, err := catalog.PrepareAgentPolicy(t.Context(), AgentMetricPolicyInput{NodeID: "missing", Values: values}); !errors.Is(err, ErrMetricIngestion) {
			t.Fatal("invalid custom policy was admitted", err)
		}
	}
	if _, err := sqlite.DB.Exec("DELETE FROM metric_definitions WHERE metric_key='cpu'"); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PrepareAgentPolicy(t.Context(), AgentMetricPolicyInput{NodeID: "missing"}); err == nil {
		t.Fatal("incomplete builtin policy was admitted")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PrepareAgentPolicy(t.Context(), AgentMetricPolicyInput{NodeID: "missing"}); err == nil {
		t.Fatal("closed policy store returned success")
	}
}
