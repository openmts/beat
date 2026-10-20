package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func customExecutionFixture(t *testing.T) (*SQLiteStore, customExecutionReference, model.CustomMetric) {
	t.Helper()
	sqlite := setupTestDB(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "custom-node", "192.0.2.20", 22)
	if err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	if _, err := sqlite.DB.ExecContext(t.Context(), `INSERT INTO custom_metrics
		(id,name,shell_command,interval_seconds,description,enabled,created_at,updated_at) VALUES ('metric-1','temp_probe','/bin/true',30,'',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(), "INSERT INTO custom_metric_nodes (metric_id,node_id) VALUES ('metric-1',?)", node.ID); err != nil {
		t.Fatal(err)
	}
	metric := model.CustomMetric{ID: "metric-1", Name: "temp_probe", ShellCommand: "/bin/true", IntervalSeconds: 30, UpdatedAt: now}
	reference := customExecutionReference{nodeID: node.ID, reference: AlertMetricPolicyReference{
		Key: model.CustomMetricPrefix + metric.Name, DefinitionVersion: model.CustomMetricDefinitionVersion(metric), CadenceMS: 30000}}
	return sqlite, reference, metric
}

func TestRecheckCustomExecutionPolicyValidatesBindingDefinitionAndCadence(t *testing.T) {
	sqlite, reference, metric := customExecutionFixture(t)
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, reference); err != nil {
		t.Fatalf("matching custom policy = %v", err)
	}

	staleCadence := reference
	staleCadence.reference.CadenceMS = 60000
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, staleCadence); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("stale cadence = %v", err)
	}
	staleVersion := reference
	staleVersion.reference.DefinitionVersion = model.CustomMetricDefinitionVersion(model.CustomMetric{ID: metric.ID, Name: metric.Name,
		ShellCommand: "/bin/false", IntervalSeconds: metric.IntervalSeconds, UpdatedAt: metric.UpdatedAt})
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, staleVersion); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("stale definition = %v", err)
	}

	unbound := reference
	unbound.nodeID = "unbound-node"
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, unbound); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("unbound node = %v", err)
	}
	allowed := unbound
	allowed.allowMissing, allowed.reference.DefinitionVersion, allowed.reference.CadenceMS = true, "", 0
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, allowed); err != nil {
		t.Fatalf("deleted custom metric with empty evidence = %v", err)
	}
	notAllowed := allowed
	notAllowed.reference.CadenceMS = 30000
	if err := recheckCustomExecutionPolicy(t.Context(), sqlite.DB, notAllowed); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("deleted custom metric with evidence = %v", err)
	}
}
