package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestCustomMetricBindingInputAndInvalidUpdateRejectedBeforeWriting(t *testing.T) {
	_, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], make([]string, maxCustomMetricTargets+1)); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("node-centric binding list was unbounded")
	}
	invalid := *metric
	invalid.ShellCommand = ""
	if _, err := metrics.UpdateWithRegistration(t.Context(), metric.ID, invalid); !errors.Is(err, ErrCustomMetricInvalid) {
		t.Fatal("invalid update reached storage")
	}
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], []string{"missing-metric"}); err == nil {
		t.Fatal("invalid binding succeeded")
	}
	installCustomMetricFault(t, metrics.db, "CREATE TRIGGER custom_binding_fault BEFORE DELETE ON custom_metric_nodes BEGIN SELECT RAISE(ABORT,'delete failed'); END")
	if _, err := metrics.db.Exec("INSERT INTO custom_metric_nodes(metric_id,node_id) VALUES(?,?)", metric.ID, nodes[0]); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetNodeBindings(t.Context(), nodes[0], nil); err == nil {
		t.Fatal("binding removal error ignored")
	}
}

func seedLegacyCustomMetricTargets(t *testing.T, metrics *CustomMetricStore, metricID string) {
	t.Helper()
	now := model.NowUTC()
	_, err := metrics.db.Exec(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<?)
		INSERT INTO nodes(id,name,host,port,created_at,updated_at)
		SELECT 'legacy-'||n,'legacy-'||n,'127.0.0.1',22,?,? FROM seq`, maxCustomMetricAffectedNodes+1, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.db.Exec("INSERT INTO custom_metric_nodes(metric_id,node_id) SELECT ?,id FROM nodes WHERE id LIKE 'legacy-%'", metricID); err != nil {
		t.Fatal(err)
	}
}

func TestCustomMetricLegacyAffectedNodesAreBoundedAndRollback(t *testing.T) {
	_, metrics, metric, nodes := customRegistrationFixture(t)
	seedLegacyCustomMetricTargets(t, metrics, metric.ID)
	if _, err := metrics.DeleteWithRegistration(t.Context(), metric.ID); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("unbounded previous bindings were accepted")
	}
	if _, err := metrics.db.Exec("DELETE FROM custom_metric_nodes WHERE metric_id=? AND node_id='legacy-1'", metric.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.SetMetricNodesWithRegistration(t.Context(), metric.ID, nodes[:1]); !errors.Is(err, ErrCustomMetricLimit) {
		t.Fatal("unbounded affected union was accepted")
	}
	ids, err := metrics.NodeIDsForMetric(t.Context(), metric.ID)
	if err != nil || len(ids) != maxCustomMetricAffectedNodes {
		t.Fatal("bounded union rejection changed previous bindings")
	}
	stored, err := metrics.Get(t.Context(), metric.ID)
	if err != nil || stored == nil {
		t.Fatal("bound rejection deleted the definition")
	}
}

type customMetricResultFailure struct{ schemaExecutor }

type customMetricCountFailure struct{ sql.Result }

var errCustomMetricCount = errors.New("task update count unavailable")

func (failure customMetricResultFailure) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := failure.schemaExecutor.ExecContext(ctx, query, args...)
	return customMetricCountFailure{Result: result}, err
}

func (customMetricCountFailure) RowsAffected() (int64, error) { return 0, errCustomMetricCount }

func TestCustomMetricTaskCountFailuresRollBackTransaction(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginWriteTx(t.Context(), sqlite.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	fault := customMetricResultFailure{schemaExecutor: tx}
	completion := AgentTaskCompletion{NodeID: nodes[0], TaskID: task.ID, Status: model.AgentTaskStatusDone}
	if err := completePendingAgentTask(t.Context(), fault, completion); !errors.Is(err, errCustomMetricCount) {
		t.Fatal("completion count error was ignored")
	}
	if err := supersedeCustomMetricTasks(t.Context(), fault, *task); !errors.Is(err, errCustomMetricCount) {
		t.Fatal("supersession count error was ignored")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	pending, err := NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), nodes[0])
	if err != nil || len(pending) != 1 || pending[0].ID != task.ID {
		t.Fatal("count error committed a partial state")
	}
}
