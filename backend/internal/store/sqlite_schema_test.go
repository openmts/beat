package store

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSQLiteSchemaContainsOnlyApplicationData(t *testing.T) {
	store := setupTestDB(t)

	rows, err := store.DB.Query(
		"SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name",
	)
	if err != nil {
		t.Fatalf("query SQLite tables: %v", err)
	}
	defer func() { _ = rows.Close() }()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan SQLite table: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate SQLite tables: %v", err)
	}

	wantTables := []string{
		"admin_audit_events", "admin_backups", "admin_sessions", "admin_users", "alert_channels", "alert_events",
		"alert_rules", "groups", "maintenance_settings",
		"network_task_nodes", "network_tasks", "nodes", "schema_migrations", "site_settings", "ssh_keys",
		"traffic_report_schedule_channels", "traffic_report_schedule_nodes", "traffic_report_schedules",
	}
	if !slices.Equal(tables, wantTables) {
		t.Fatalf("SQLite tables = %v, want application tables %v", tables, wantTables)
	}

	nodeColumns := []string{
		"agent_token_created_at", "agent_token_hash", "agent_token_last_used_at",
		"agent_token_prefix", "agent_token_revoked_at", "agent_version", "alias", "arch",
		"cpu_model", "created_at", "group_id", "host", "id", "is_public",
		"kernel", "last_seen", "name", "os", "os_version", "platform", "port", "private_remark",
		"public_remark", "sort_order", "ssh_public_key", "status", "tags", "traffic_limit",
		"traffic_limit_type", "traffic_reset_day", "updated_at", "virtualization",
	}
	rows, err = store.DB.Query("SELECT name FROM pragma_table_info('nodes') ORDER BY name")
	if err != nil {
		t.Fatalf("query node columns: %v", err)
	}
	defer func() { _ = rows.Close() }()

	columns := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan node column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate node columns: %v", err)
	}
	if !slices.Equal(columns, nodeColumns) {
		t.Fatalf("node columns = %v, want application columns %v", columns, nodeColumns)
	}

	wantNetworkColumns := []string{
		"all_nodes", "created_at", "enabled", "id", "interval_seconds", "ip_family",
		"is_public", "name", "sort_order", "target", "timeout_milliseconds", "type", "updated_at",
	}
	rows, err = store.DB.Query("SELECT name FROM pragma_table_info('network_tasks') ORDER BY name")
	if err != nil {
		t.Fatalf("query network task columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns = columns[:0]
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan network task column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate network task columns: %v", err)
	}
	if !slices.Equal(columns, wantNetworkColumns) {
		t.Fatalf("network task columns = %v, want application columns %v", columns, wantNetworkColumns)
	}
}

func TestSQLiteSchemaExcludesTelemetryColumns(t *testing.T) {
	store := setupTestDB(t)
	columns, err := sqliteSchemaColumns(t.Context(), store.DB)
	if err != nil {
		t.Fatalf("query SQLite schema columns: %v", err)
	}

	forbidden := append(model.MetricNames(),
		"net_recv_delta", "net_sent_delta", "latency_ms", "success", "status_code", "error_code")
	for _, name := range forbidden {
		if table, exists := columns[name]; exists {
			t.Fatalf("SQLite application table %q contains telemetry column %q", table, name)
		}
	}
}

func sqliteSchemaColumns(ctx context.Context, database *sql.DB) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, `SELECT m.name, p.name
		FROM sqlite_master AS m JOIN pragma_table_info(m.name) AS p
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns := map[string]string{}
	for rows.Next() {
		var table string
		var column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, err
		}
		columns[column] = table
	}
	return columns, rows.Err()
}
