package backup

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const erasureLedgerJobsSchema = `CREATE TABLE metric_erasure_jobs(
	id TEXT, scope TEXT, target_type TEXT, target_id TEXT, target_name TEXT, state TEXT,
	cutoff_unix_nano INTEGER, measurement_cursor TEXT, attempt_count INTEGER,
	last_error_code TEXT, request_id TEXT, requested_at DATETIME, completed_at DATETIME)`

func erasureLedgerSnapshot(t *testing.T, statements ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("seed archived snapshot (%s): %v", statement, err)
		}
	}
	return path
}

var erasureLedgerPreamble = []string{
	"CREATE TABLE schema_migrations(version INTEGER)",
	"INSERT INTO schema_migrations(version) VALUES(22)",
}

func erasureLedgerStatements(extra ...string) []string {
	return append(append([]string{}, erasureLedgerPreamble...), extra...)
}

// TestMetricErasureLedgerReadFaults 覆盖归档擦除账本读取阶段的失败关闭与旧快照的兼容分支。
func TestMetricErasureLedgerReadFaults(t *testing.T) {
	t.Run("schema version unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, "CREATE TABLE schema_migrations(id TEXT)")
		if handled, err := validateMetricErasureLedger(t.Context(), path); handled || err == nil {
			t.Fatalf("unreadable schema version = %v/%v", handled, err)
		}
	})
	t.Run("ledger unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements()...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("missing ledger table = %v/%v", handled, err)
		}
	})
	t.Run("node identifiers unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema)...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("missing node table = %v/%v", handled, err)
		}
	})
	t.Run("task identifiers unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema,
			"CREATE TABLE nodes(id TEXT)", "CREATE TABLE network_tasks(task TEXT)")...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("unreadable task identifiers = %v/%v", handled, err)
		}
	})
	t.Run("missing task table tolerated", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema,
			"CREATE TABLE nodes(id TEXT)")...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err != nil {
			t.Fatalf("missing task table = %v/%v", handled, err)
		}
	})
}

// TestMetricErasureLedgerCursorFaults 覆盖任务与节点/质量任务游标的扫描失败与迭代失败。
func TestMetricErasureLedgerCursorFaults(t *testing.T) {
	t.Run("task scan failure", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema,
			"INSERT INTO metric_erasure_jobs(id) VALUES(NULL)",
			"CREATE TABLE nodes(id TEXT)", "CREATE TABLE network_tasks(id TEXT)")...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("NULL task cursor = %v/%v", handled, err)
		}
	})
	t.Run("task iteration failure", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(
			`CREATE TABLE cov_erasure_jobs(id TEXT, scope TEXT, target_type TEXT, target_id TEXT,
			 target_name TEXT, state TEXT, cutoff_unix_nano INTEGER, measurement_cursor TEXT,
			 attempt_count INTEGER, last_error_code TEXT, request_id TEXT, requested_at DATETIME,
			 completed_at DATETIME)`,
			`INSERT INTO cov_erasure_jobs(id, attempt_count) VALUES('11111111-1111-4111-8111-111111111111', 0)`,
			`INSERT INTO cov_erasure_jobs(id, attempt_count) VALUES('11111111-1111-4111-8111-111111111112', 9)`,
			`CREATE VIEW metric_erasure_jobs AS SELECT id, scope, target_type, target_id, target_name,
			 state, cutoff_unix_nano, measurement_cursor, CASE WHEN attempt_count = 9
			 THEN abs(-9223372036854775808) ELSE attempt_count END AS attempt_count,
			 last_error_code, request_id, requested_at, completed_at, rowid FROM cov_erasure_jobs`,
			"CREATE TABLE nodes(id TEXT)", "CREATE TABLE network_tasks(id TEXT)")...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("expired task cursor = %v/%v", handled, err)
		}
	})
	t.Run("task identifier scan failure", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema,
			"CREATE TABLE nodes(id TEXT)", "CREATE TABLE network_tasks(id TEXT)",
			"INSERT INTO network_tasks(id) VALUES(NULL)")...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("NULL task identifier = %v/%v", handled, err)
		}
	})
	t.Run("task identifier iteration failure", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, erasureLedgerStatements(erasureLedgerJobsSchema,
			"CREATE TABLE nodes(id TEXT)",
			"CREATE TABLE cov_task_rows(id TEXT)", "INSERT INTO cov_task_rows(id) VALUES('a')",
			"INSERT INTO cov_task_rows(id) VALUES('boom')",
			`CREATE VIEW network_tasks AS SELECT CASE WHEN id = 'boom'
			 THEN abs(-9223372036854775808) ELSE id END AS id FROM cov_task_rows`)...)
		if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
			t.Fatalf("expired task identifier cursor = %v/%v", handled, err)
		}
	})
}

func validArchivedErasureJob() model.MetricErasureJob {
	const id = "11111111-1111-4111-8111-111111111111"
	return model.MetricErasureJob{ID: id, RequestID: id, Scope: model.MetricErasureNodeHistory,
		TargetType: model.MetricErasureTargetNode, TargetID: "22222222-2222-4222-8222-222222222222",
		TargetName: "server-one", State: model.MetricErasureQueued,
		RequestedAt: time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)}
}

// TestArchivedMetricErasureJobRejections 逐条覆盖归档擦除任务的状态与实体关联校验。
func TestArchivedMetricErasureJobRejections(t *testing.T) {
	cutoff := time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC).UnixNano()
	completed := time.Date(2026, time.October, 15, 9, 30, 0, 0, time.UTC)
	nodes := map[string]bool{"22222222-2222-4222-8222-222222222222": true}
	tasks := map[string]bool{"33333333-3333-4333-8333-333333333333": true}
	cases := map[string]func(job *model.MetricErasureJob){
		"zero clock":          func(job *model.MetricErasureJob) { job.RequestedAt = time.Time{} },
		"attempts over limit": func(job *model.MetricErasureJob) { job.AttemptCount = store.MaximumMetricErasureAttempts + 1 },
		"terminal without time": func(job *model.MetricErasureJob) {
			job.State = model.MetricErasureSucceeded
			job.CutoffUnixNano = &cutoff
		},
		"active with time": func(job *model.MetricErasureJob) { job.CompletedAt = &completed },
		"failed without code": func(job *model.MetricErasureJob) {
			job.State = model.MetricErasureFailed
			job.CompletedAt = &completed
		},
		"deleting without cutoff": func(job *model.MetricErasureJob) { job.State = model.MetricErasureDeleting },
		"node deletion without node": func(job *model.MetricErasureJob) {
			job.Scope = model.MetricErasureDeleteNode
			job.TargetID = "00000000-0000-4000-8000-000000000000"
		},
		"task history without task": func(job *model.MetricErasureJob) {
			job.Scope = model.MetricErasureNetworkTaskHistory
			job.TargetType = model.MetricErasureTargetNetworkTask
			job.TargetID = "00000000-0000-4000-8000-000000000001"
		},
		"task deletion without task": func(job *model.MetricErasureJob) {
			job.Scope = model.MetricErasureDeleteNetworkTask
			job.TargetType = model.MetricErasureTargetNetworkTask
			job.TargetID = "00000000-0000-4000-8000-000000000002"
		},
		"fleet job with target": func(job *model.MetricErasureJob) {
			job.Scope = model.MetricErasureFleetTelemetry
			job.TargetType = model.MetricErasureTargetNode
		},
	}
	if err := validateArchivedMetricErasureJob(validArchivedErasureJob(), nodes, tasks); err != nil {
		t.Fatalf("valid archived job error = %v", err)
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			job := validArchivedErasureJob()
			mutate(&job)
			if err := validateArchivedMetricErasureJob(job, nodes, tasks); err == nil {
				t.Fatalf("archived job %s was accepted", name)
			}
		})
	}
}
