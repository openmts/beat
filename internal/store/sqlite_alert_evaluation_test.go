package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func migrateAlertEvaluationFixture(t *testing.T, sqlite *SQLiteStore) {
	t.Helper()
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := MigrateAlertEvaluation(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertEvaluationMigrationBackfillsPolicyWithoutRegisteringLedger(t *testing.T) {
	sqlite := preAlertEvaluationFixture(t)
	rule, err := NewAlertRuleStore(sqlite.DB).CreateAlertRule(t.Context(), &model.AlertRule{Name: "CPU", Metric: "cpu", Operator: "gt", Threshold: 80, Duration: 60, Severity: model.AlertSeverityWarning, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateAlertEvaluationFixture(t, sqlite)
	var revision, window, recoveryDuration int
	var scope, aggregation, noData, errorPolicy string
	if err := sqlite.DB.QueryRow(`SELECT revision,scope_kind,window_seconds,aggregation,recovery_duration,no_data_policy,error_policy FROM alert_rules WHERE id=?`, rule.ID).
		Scan(&revision, &scope, &window, &aggregation, &recoveryDuration, &noData, &errorPolicy); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || scope != "all" || window != 0 || aggregation != "last" || recoveryDuration != 0 || noData != "keep" || errorPolicy != "keep" {
		t.Fatal("old alert policy changed unexpectedly")
	}
	var maximum int
	if err := sqlite.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
		t.Fatal(err)
	}
	if maximum != alertScopeSchemaVersion-1 {
		t.Fatal("independent v12 DDL registered the ledger")
	}
	for _, table := range []string{"alert_rule_nodes", "alert_rule_groups", "alert_evaluation_states", "alert_evaluation_events", "traffic_report_preparations"} {
		var count int
		if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatal("missing v12 table", table, err)
		}
	}
}

func TestAlertEvaluationMigrationRollsBackFailureAndRetries(t *testing.T) {
	sqlite := preAlertEvaluationFixture(t)
	if _, err := sqlite.DB.Exec("CREATE TABLE alert_evaluation_states(blocker INTEGER)"); err != nil {
		t.Fatal(err)
	}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateAlertEvaluation(t.Context(), tx); err == nil {
		_ = tx.Rollback()
		t.Fatal("expected DDL conflict")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('alert_rules') WHERE name='revision'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration partially added policy", err)
	}
	if _, err := sqlite.DB.Exec("DROP TABLE alert_evaluation_states"); err != nil {
		t.Fatal(err)
	}
	migrateAlertEvaluationFixture(t, sqlite)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := MigrateAlertEvaluation(ctx, sqlite.DB); err == nil {
		t.Fatal("cancelled migration succeeded")
	}
}

// 使用真实历史迁移构造v11，验证升级；不修改生产迁移表或全局迁移切片。
func preAlertEvaluationFixture(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-v12.db")
	if err := secureSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at DATETIME NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range sqliteMigrations {
		if migration.version >= alertScopeSchemaVersion {
			break
		}
		if err := applyMigration(t.Context(), db, migration); err != nil {
			t.Fatal(err)
		}
	}
	return &SQLiteStore{DB: db, path: path}
}
