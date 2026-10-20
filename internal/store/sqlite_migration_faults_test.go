package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covSchemaFault 是 schemaExecutor 故障注入器：第 failAt 次调用起返回注入错误。
// 用来验证每条迁移与每个建表辅助函数在 DDL 失败时失败关闭，而不是静默跳过。
type covSchemaFault struct {
	base   schemaExecutor
	closed *sql.DB
	failAt int
	calls  int
	reason error
}

func newCovSchemaFault(t *testing.T, base schemaExecutor, failAt int) *covSchemaFault {
	t.Helper()
	return &covSchemaFault{base: base, closed: covClosedHandle(t), failAt: failAt, reason: errors.New("cov schema fault")}
}

func (fault *covSchemaFault) next() error {
	fault.calls++
	if fault.calls >= fault.failAt {
		return fault.reason
	}
	return nil
}

func (fault *covSchemaFault) triggered() bool { return fault.calls >= fault.failAt }

func (fault *covSchemaFault) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := fault.next(); err != nil {
		return nil, err
	}
	return fault.base.ExecContext(ctx, query, args...)
}

func (fault *covSchemaFault) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if err := fault.next(); err != nil {
		return nil, err
	}
	return fault.base.QueryContext(ctx, query, args...)
}

func (fault *covSchemaFault) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if err := fault.next(); err != nil {
		return fault.closed.QueryRowContext(ctx, query, args...)
	}
	return fault.base.QueryRowContext(ctx, query, args...)
}

func covTempHandle(t *testing.T, name string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	handle, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}

func covNoopMigration(version int) sqliteMigration {
	return sqliteMigration{version: version, name: "cov_noop", apply: func(context.Context, schemaExecutor) error { return nil }}
}

func TestSchemaHelpersFailClosedOnExecutorFaults(t *testing.T) {
	store := setupTestDB(t)
	helpers := []struct {
		name string
		run  func(context.Context, schemaExecutor) error
	}{
		{"create tables", createTables},
		{"execute statements", func(ctx context.Context, db schemaExecutor) error {
			return executeSchemaStatements(ctx, db, platformSchemaStatements)
		}},
		{"ensure default group", ensureDefaultGroup},
		{"insert default group", func(ctx context.Context, db schemaExecutor) error {
			return insertDefaultGroup(ctx, db, "cov-default-group")
		}},
	}
	for _, helper := range helpers {
		fault := newCovSchemaFault(t, store.DB, 1)
		if err := helper.run(t.Context(), fault); err == nil {
			t.Fatalf("%s accepted a failing executor", helper.name)
		}
		if !fault.triggered() {
			t.Fatalf("%s never reached the executor", helper.name)
		}
	}
}

func TestMigrationApplyFunctionsFailClosedOnExecutorFaults(t *testing.T) {
	base := setupTestDB(t)
	for _, migration := range sqliteMigrations {
		for failAt := 1; failAt <= 3; failAt++ {
			fault := newCovSchemaFault(t, base.DB, failAt)
			err := migration.apply(t.Context(), fault)
			if fault.triggered() && err == nil {
				t.Fatalf("migration %d accepted a schema fault at call %d", migration.version, failAt)
			}
		}
	}
}

func TestMigrationLedgerFailuresFailClosed(t *testing.T) {
	closed := covClosedHandle(t)
	if err := applyMigrations(t.Context(), closed); err == nil {
		t.Fatal("closed database accepted migrations")
	}
	if err := applyMigrationsObserved(t.Context(), closed, nil); err == nil {
		t.Fatal("closed database accepted observed migrations")
	}
	if _, err := currentMigrationVersion(t.Context(), closed); err == nil {
		t.Fatal("closed database reported a migration version")
	}
	if _, err := migrationApplied(t.Context(), closed, 1); err == nil {
		t.Fatal("closed database reported an applied migration")
	}
	future := setupTestDB(t)
	if _, err := future.DB.Exec("INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)",
		currentSchemaVersion+1, "future", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if code := MigrationFailureCode(applyMigrationsObserved(t.Context(), future.DB, nil)); code != MigrationFailureFutureSchema {
		t.Fatalf("future schema code = %q", code)
	}
	broken := covTempHandle(t, "broken-ledger.db")
	if _, err := broken.Exec("CREATE TABLE schema_migrations (name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationsObserved(t.Context(), broken, nil); err == nil {
		t.Fatal("ledger without a version column must fail")
	}
}

func TestApplyMigrationFailsClosedOnApplyLedgerAndCommitFaults(t *testing.T) {
	store := setupTestDB(t)
	if err := applyMigration(t.Context(), covClosedHandle(t), covNoopMigration(90)); err == nil {
		t.Fatal("closed database accepted a migration transaction")
	}
	failing := sqliteMigration{version: 90, name: "cov_failing", apply: func(context.Context, schemaExecutor) error {
		return errors.New("cov apply fault")
	}}
	if code := MigrationFailureCode(applySingleMigration(t.Context(), store.DB, failing, nil)); code != MigrationFailureApply {
		t.Fatalf("apply failure code = %q", code)
	}
	if err := applyMigration(t.Context(), store.DB, failing); err == nil {
		t.Fatal("failing migration apply reported success")
	}
	if _, err := store.DB.Exec(`CREATE TRIGGER cov_block_ledger BEFORE INSERT ON schema_migrations
		BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	err := applyMigration(t.Context(), store.DB, covNoopMigration(91))
	if err == nil || !strings.Contains(err.Error(), "record migration") {
		t.Fatalf("blocked ledger insert must fail the migration: %v", err)
	}
}

func TestApplyMigrationReportsCommitTimeFailures(t *testing.T) {
	store := setupTestDB(t)
	if _, err := store.DB.Exec("CREATE TABLE cov_commit_guard(parent TEXT REFERENCES admin_users(id) DEFERRABLE INITIALLY DEFERRED)"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`CREATE TRIGGER cov_commit_guard AFTER INSERT ON schema_migrations
		BEGIN INSERT INTO cov_commit_guard(parent) VALUES('missing'); END`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(t.Context(), store.DB, covNoopMigration(92)); err == nil {
		t.Fatal("commit-time constraint violation must fail the migration")
	}
}

type covMigrationObserver struct {
	started   int
	applied   int
	completed int
	failures  []string
	rollbacks []bool
}

func (observer *covMigrationObserver) MigrationStarted(int) { observer.started++ }

func (observer *covMigrationObserver) MigrationApplied(Migration, int) {
	observer.applied++
}

func (observer *covMigrationObserver) MigrationCompleted(int) { observer.completed++ }

func (observer *covMigrationObserver) MigrationFailed(code string, rollback bool) {
	observer.failures = append(observer.failures, code)
	observer.rollbacks = append(observer.rollbacks, rollback)
}

func TestMigrationObserverReceivesBoundedStages(t *testing.T) {
	observer := &covMigrationObserver{}
	store, err := NewSQLiteStoreObserved(t.Context(), filepath.Join(t.TempDir(), "observed.db"), observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if observer.started != 1 || observer.completed != 1 || observer.applied != len(sqliteMigrations) {
		t.Fatalf("observer stages = started %d applied %d completed %d", observer.started, observer.applied, observer.completed)
	}
	if len(observer.failures) != 0 {
		t.Fatalf("healthy migration reported failures: %v", observer.failures)
	}
	failing := &covMigrationObserver{}
	futurePath := filepath.Join(t.TempDir(), "future-observed.db")
	handle, err := sql.Open("sqlite", sqliteConnectionString(futurePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec("INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)",
		currentSchemaVersion+1, "future", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(futurePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStoreObserved(t.Context(), futurePath, failing); err == nil {
		t.Fatal("future schema must fail startup")
	}
	if len(failing.failures) != 1 || failing.failures[0] != MigrationFailureFutureSchema || !failing.rollbacks[0] {
		t.Fatalf("future schema failure report = %#v", failing)
	}
}
