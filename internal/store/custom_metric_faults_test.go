package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func installCustomMetricFault(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func installCustomMetricCommitFault(t *testing.T, db *sql.DB, target string) {
	t.Helper()
	installCustomMetricFault(t, db,
		"CREATE TABLE custom_fault_parent(id TEXT PRIMARY KEY)",
		"CREATE TABLE custom_fault_child(parent TEXT REFERENCES custom_fault_parent(id) DEFERRABLE INITIALLY DEFERRED)",
		"CREATE TRIGGER custom_commit_failure AFTER "+target+" BEGIN INSERT INTO custom_fault_child VALUES('missing'); END")
}

func TestCustomMetricMutationFailuresRollBackDefinitionBindingsAndQueue(t *testing.T) {
	for _, statement := range []string{
		"CREATE TRIGGER custom_fault BEFORE UPDATE ON custom_metrics BEGIN SELECT RAISE(ABORT,'write failed'); END",
		"CREATE TRIGGER custom_fault BEFORE INSERT ON agent_tasks BEGIN SELECT RAISE(ABORT,'queue failed'); END",
		"CREATE TRIGGER custom_fault BEFORE UPDATE ON agent_tasks BEGIN SELECT RAISE(ABORT,'supersede failed'); END",
		"CREATE TRIGGER custom_fault BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END",
		"commit",
	} {
		t.Run(statement, func(t *testing.T) {
			sqlite, metrics, metric, nodes := customRegistrationFixture(t)
			if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
				t.Fatal(err)
			}
			if statement == "commit" {
				installCustomMetricCommitFault(t, sqlite.DB, "INSERT ON agent_tasks")
			} else {
				installCustomMetricFault(t, sqlite.DB, statement)
			}
			changed := *metric
			changed.Enabled = false
			if _, err := metrics.UpdateWithRegistration(t.Context(), metric.ID, changed); err == nil {
				t.Fatal("storage failure was acknowledged")
			}
			assertCustomMetricRollback(t, metrics, metric)
		})
	}
}

func assertCustomMetricRollback(t *testing.T, metrics *CustomMetricStore, original *model.CustomMetric) {
	t.Helper()
	stored, err := metrics.Get(t.Context(), original.ID)
	if err != nil || stored == nil || stored.Enabled != original.Enabled {
		t.Fatal("failed mutation changed definition")
	}
	ids, err := metrics.NodeIDsForMetric(t.Context(), original.ID)
	if err != nil || len(ids) != 1 {
		t.Fatal("failed mutation changed bindings")
	}
	var tasks, pending, audits int
	if err := metrics.db.QueryRow("SELECT COUNT(*),COUNT(CASE WHEN status='pending' THEN 1 END) FROM agent_tasks").Scan(&tasks, &pending); err != nil {
		t.Fatal(err)
	}
	if err := metrics.db.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='agent.custom_metrics.supersede'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || pending != 1 || audits != 0 {
		t.Fatalf("failed mutation left task/audit changes: %d/%d/%d", tasks, pending, audits)
	}
}

func TestCustomMetricBindingWriteFailuresRollBack(t *testing.T) {
	for _, operation := range []string{"delete", "insert", "foreign key", "node queue", "node commit"} {
		t.Run(operation, func(t *testing.T) {
			sqlite, metrics, metric, nodes := customRegistrationFixture(t)
			if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "delete", "insert":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER custom_binding_fault BEFORE "+strings.ToUpper(operation)+" ON custom_metric_nodes BEGIN SELECT RAISE(ABORT,'binding failed'); END")
				_, err = metrics.SetMetricNodesWithRegistration(t.Context(), metric.ID, nodes[1:])
			case "foreign key":
				_, err = metrics.SetMetricNodesWithRegistration(t.Context(), metric.ID, []string{"missing-node"})
			case "node queue":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER custom_queue_fault BEFORE INSERT ON agent_tasks BEGIN SELECT RAISE(ABORT,'queue failed'); END")
				err = metrics.SetNodeBindings(t.Context(), nodes[0], nil)
			case "node commit":
				installCustomMetricCommitFault(t, sqlite.DB, "INSERT ON agent_tasks")
				err = metrics.SetNodeBindings(t.Context(), nodes[0], nil)
			}
			if err == nil {
				t.Fatal("binding failure was acknowledged")
			}
			assertCustomMetricRollback(t, metrics, metric)
		})
	}
}

func TestCustomMetricQueueCommitAndMissingNodeFailures(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	if _, err := metrics.QueueNodeCustomMetrics(t.Context(), "missing"); err == nil {
		t.Fatal("queued a snapshot for an unknown node")
	}
	installCustomMetricCommitFault(t, sqlite.DB, "INSERT ON agent_tasks")
	if _, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0]); err == nil {
		t.Fatal("registration commit failure ignored")
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_tasks").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed queue retained task")
	}
}

func TestCustomMetricCanceledAndClosedStorageFailsBeforeMutation(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.UpdateWithRegistration(ctx, metric.ID, *metric); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled metadata write proceeded")
	}
	if _, err := metrics.QueueNodeCustomMetrics(ctx, nodes[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled queue proceeded")
	}
	if err := metrics.SetNodeBindings(ctx, nodes[0], nil); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled binding write proceeded")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.Create(t.Context(), *metric); err == nil {
		t.Fatal("closed store created definition")
	}
}

type customMetricReadFault struct {
	schemaExecutor
	rows   *sql.DB
	rowSQL string
}

func (fault customMetricReadFault) QueryContext(ctx context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return fault.rows.QueryContext(ctx, fault.rowSQL)
}

func TestCustomMetricSnapshotRejectsReadAndScanFailures(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"SELECT * FROM missing_table", "SELECT NULL"} {
		fault := customMetricReadFault{schemaExecutor: sqlite.DB, rows: sqlite.DB, rowSQL: query}
		if _, err := customMetricNodeIDs(t.Context(), fault, metric.ID); err == nil {
			t.Fatal("binding read failure ignored")
		}
		if _, err := customMetricExecutionSnapshot(t.Context(), fault, nodes[0]); err == nil {
			t.Fatal("snapshot read failure ignored")
		}
	}
	installCustomMetricFault(t, sqlite.DB, "ALTER TABLE agent_tasks RENAME COLUMN created_at TO broken_revision")
	if _, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0]); err == nil {
		t.Fatal("snapshot revision read failure ignored")
	}
}

func TestCustomMetricSnapshotRejectsCorruptLegacyDefinitions(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE custom_metrics SET name=? WHERE id=?", "invalid/name", metric.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0]); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatal("invalid legacy definition was queued")
	}
}
