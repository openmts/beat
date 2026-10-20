package store

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func customRegistrationFixture(t *testing.T) (*SQLiteStore, *CustomMetricStore, *model.CustomMetric, []string) {
	t.Helper()
	sqlite := setupTestDB(t)
	metrics := NewCustomMetricStore(sqlite.DB)
	metric, err := metrics.Create(t.Context(), validCustomMetric())
	if err != nil {
		t.Fatal(err)
	}
	nodes := NewNodeStore(sqlite.DB)
	ids := make([]string, 0, 3)
	for _, name := range []string{"previous", "retained", "added"} {
		node, err := nodes.UpsertNode(t.Context(), name, "127.0.0.1", 22)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, node.ID)
	}
	return sqlite, metrics, metric, ids
}

func TestCustomMetricRegistrationIncludesPreviousAndNewNodes(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:2]); err != nil {
		t.Fatal(err)
	}
	change, err := metrics.SetMetricNodesWithRegistration(t.Context(), metric.ID, nodes[1:])
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Tasks) != 3 {
		t.Fatalf("affected snapshots=%d", len(change.Tasks))
	}
	for _, task := range change.Tasks {
		payload, err := model.DecodeCustomMetricRegisterPayload(task.Payload)
		if err != nil {
			t.Fatal(err)
		}
		expected := 1
		if task.NodeID == nodes[0] {
			expected = 0
		}
		if len(payload.Metrics) != expected {
			t.Fatalf("node snapshot has %d definitions, expected %d", len(payload.Metrics), expected)
		}
	}
	var superseded, audits int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_tasks WHERE type=? AND status='failed' AND completed_at IS NOT NULL", model.AgentTaskCustomMetricRegister).Scan(&superseded); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='agent.custom_metrics.supersede'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if superseded != 2 || audits != 2 {
		t.Fatalf("superseded=%d audit=%d", superseded, audits)
	}
}

func TestCustomMetricDeletionQueuesEmptyRegistrationAtomically(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`CREATE TRIGGER fail_custom_registration BEFORE INSERT ON agent_tasks BEGIN SELECT RAISE(ABORT,'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.DeleteWithRegistration(t.Context(), metric.ID); err == nil {
		t.Fatal("deletion bypassed queue failure")
	}
	if existing, err := metrics.Get(t.Context(), metric.ID); err != nil || existing == nil {
		t.Fatal("failed revocation deleted definition")
	}
	if _, err := sqlite.DB.Exec("DROP TRIGGER fail_custom_registration"); err != nil {
		t.Fatal(err)
	}
	change, err := metrics.DeleteWithRegistration(t.Context(), metric.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Tasks) != 1 || !strings.Contains(change.Tasks[0].Payload, `"metrics":[]`) {
		t.Fatal("deletion did not persist an empty snapshot")
	}
}

func TestCustomMetricRegistrationsRemainOrderedAcrossClockRegression(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	first, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := sqlite.DB.Exec("UPDATE agent_tasks SET created_at=? WHERE id=?", future, first.ID); err != nil {
		t.Fatal(err)
	}
	next, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !next.CreatedAt.After(future) {
		t.Fatal("new snapshot can be overtaken by an older queued task")
	}
	pending, err := NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), nodes[0])
	if err != nil || len(pending) != 1 || pending[0].ID != next.ID {
		t.Fatalf("pending snapshot not coalesced: %v", err)
	}
}
