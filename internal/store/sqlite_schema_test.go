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
		"admin_audit_events", "admin_backup_quarantine", "admin_backups", "admin_external_identities", "admin_oidc_providers",
		"admin_oidc_transactions", "admin_sessions", "admin_users", "agent_enrollment_challenges", "agent_enrollment_grants", "agent_enrollment_identities", "agent_enrollment_requests", "agent_enrollment_tokens", "agent_tasks", "alert_channel_revisions", "alert_channels",
		"alert_events", "alert_rules", "application_data_keys", "application_secret_state",
		"backup_upload_chunks", "backup_upload_sessions",
		"diagnostic_attempts", "diagnostic_budget_ledger", "diagnostic_capabilities", "diagnostic_hop_identities",
		"diagnostic_jobs", "diagnostic_policies", "diagnostic_targets", "custom_metric_nodes", "custom_metrics", "geoip_cache", "geoip_settings", "groups", "lifecycle_settings", "node_lifecycle", "node_lifecycle_events", "node_locations",
		"maintenance_settings", "metric_catalog_state", "metric_dashboards", "metric_definitions", "metric_retention_jobs",
		"network_task_nodes", "network_tasks", "nodes", "notification_channel_health", "notification_deliveries", "notification_delivery_attempts", "notification_messages", "notification_request_budgets", "notification_secret_state", "notification_settings", "schema_migrations",
		"site_settings", "public_telemetry_policy", "ssh_keys", "theme_activations", "theme_market_sources", "theme_packages",
		"traffic_report_schedule_channels", "traffic_report_schedule_nodes", "traffic_report_schedules",
		"traffic_calibration_events", "metric_erasure_jobs",
		"fleet_summary_channels", "fleet_summary_groups", "fleet_summary_nodes",
		"fleet_summary_runs", "fleet_summary_schedules", "application_retention_settings",
	}
	wantTables = append(wantTables, "node_accelerators", "node_accelerator_state", "node_accelerator_visibility", "accelerator_collection_policies", "accelerator_ingest_pending", "alert_evaluation_events", "alert_evaluation_states", "alert_rule_groups", "alert_rule_nodes", "traffic_report_preparations",
		"operation_artifact_chunks", "operation_artifacts", "operation_attempts", "operation_capabilities", "operation_policies", "operation_policy_groups", "operation_policy_nodes",
		"operation_snippet_versions", "operation_snippets", "operation_task_targets", "operation_tasks", "operation_transfer_targets", "operation_transfers",
		"agent_release_signature_policies", "agent_release_signature_state", "agent_release_trust", "agent_releases", "agent_release_artifacts", "agent_release_retention",
		"agent_rollouts", "agent_rollout_targets", "agent_rollout_attempts", "agent_rollout_events",
		"service_principals", "service_token_scopes", "service_token_resources",
		"security_events", "security_login_fingerprints", "security_login_thresholds", "security_notification_settings",
		"dashboard_annotations", "dashboard_panel_alerts", "dashboard_panel_library", "dashboard_snapshots", "dashboard_versions")
	slices.Sort(wantTables)
	if !slices.Equal(tables, wantTables) {
		t.Fatalf("SQLite tables = %v, want application tables %v", tables, wantTables)
	}

	nodeColumns := []string{
		"agent_token_created_at", "agent_token_hash", "agent_token_last_used_at",
		"agent_token_prefix", "agent_token_revoked_at", "agent_version", "alias", "arch",
		"cpu_model", "created_at", "credential_state", "group_id", "host", "id", "is_public",
		"kernel", "last_seen", "lifecycle_state", "name", "os", "os_version", "platform", "port", "private_remark",
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
		"is_public", "lifecycle_state", "name", "sort_order", "target", "timeout_milliseconds", "type", "updated_at",
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
		if notificationOperationColumn(table, column) {
			continue
		}
		columns[column] = table
	}
	return columns, rows.Err()
}

func notificationOperationColumn(table, column string) bool {
	if column != "error_code" {
		return false
	}
	switch table {
	case "notification_channel_health", "notification_deliveries", "notification_delivery_attempts", "node_locations":
		return true
	}
	return false
}

func TestNotificationOperationCodeExceptionDoesNotPermitTelemetry(t *testing.T) {
	database := setupTestDB(t)
	if _, err := database.DB.Exec("CREATE TABLE rogue_metric_samples(error_code TEXT)"); err != nil {
		t.Fatal(err)
	}
	columns, err := sqliteSchemaColumns(t.Context(), database.DB)
	if err != nil {
		t.Fatal(err)
	}
	if columns["error_code"] != "rogue_metric_samples" {
		t.Fatal("notification metadata exception hid another table's telemetry")
	}
	if notificationOperationColumn("notification_deliveries", "latency_ms") || notificationOperationColumn("unregistered_notifications", "error_code") {
		t.Fatal("operation metadata exception is too broad")
	}
}
