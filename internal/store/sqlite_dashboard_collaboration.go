package store

import "context"

// applyDashboardCollaboration 引入 v25 看板协作面：注释、快照、个人面板库与面板级告警关联。
func applyDashboardCollaboration(ctx context.Context, db schemaExecutor) error {
	for _, statement := range dashboardCollaborationSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

var dashboardCollaborationSchema = []string{
	`CREATE TABLE IF NOT EXISTS dashboard_annotations (
		id TEXT PRIMARY KEY,
		dashboard_id TEXT NOT NULL CHECK(length(CAST(dashboard_id AS BLOB)) BETWEEN 1 AND 80),
		panel_id TEXT NOT NULL DEFAULT '' CHECK(length(CAST(panel_id AS BLOB)) <= 64),
		title TEXT NOT NULL CHECK(length(CAST(title AS BLOB)) BETWEEN 1 AND 200),
		text TEXT NOT NULL DEFAULT '' CHECK(length(CAST(text AS BLOB)) <= 2000),
		tags_json TEXT NOT NULL CHECK(json_valid(tags_json) AND length(CAST(tags_json AS BLOB)) <= 512),
		color TEXT NOT NULL CHECK(color IN ('blue','green','amber','rose','violet','cyan','slate')),
		visibility TEXT NOT NULL CHECK(visibility IN ('private','public')),
		from_ts DATETIME NOT NULL, to_ts DATETIME NOT NULL,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		created_by TEXT NOT NULL, updated_by TEXT NOT NULL,
		CHECK(to_ts >= from_ts))`,
	`CREATE INDEX IF NOT EXISTS idx_dashboard_annotations_dashboard
		ON dashboard_annotations(dashboard_id, from_ts)`,
	`CREATE TABLE IF NOT EXISTS dashboard_snapshots (
		id TEXT PRIMARY KEY,
		token TEXT NOT NULL UNIQUE CHECK(length(token) = 64),
		name TEXT NOT NULL CHECK(length(CAST(name AS BLOB)) BETWEEN 1 AND 200),
		dashboard_id TEXT NOT NULL CHECK(length(CAST(dashboard_id AS BLOB)) BETWEEN 1 AND 80),
		revision INTEGER NOT NULL CHECK(revision > 0),
		definition_json TEXT NOT NULL CHECK(json_valid(definition_json)
			AND length(CAST(definition_json AS BLOB)) <= 65536),
		expires_at DATETIME, revoked_at DATETIME,
		created_at DATETIME NOT NULL, created_by TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_dashboard_snapshots_owner ON dashboard_snapshots(created_by, created_at)`,
	`CREATE TABLE IF NOT EXISTS dashboard_panel_library (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
		name TEXT NOT NULL CHECK(length(CAST(name AS BLOB)) BETWEEN 1 AND 200),
		description TEXT NOT NULL DEFAULT '' CHECK(length(CAST(description AS BLOB)) <= 512),
		panel_json TEXT NOT NULL CHECK(json_valid(panel_json) AND length(CAST(panel_json AS BLOB)) <= 16384),
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_dashboard_panel_library_owner ON dashboard_panel_library(owner_user_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS dashboard_panel_alerts (
		dashboard_id TEXT NOT NULL CHECK(length(CAST(dashboard_id AS BLOB)) BETWEEN 1 AND 80),
		panel_id TEXT NOT NULL CHECK(length(CAST(panel_id AS BLOB)) BETWEEN 1 AND 64),
		rule_id TEXT NOT NULL CHECK(length(CAST(rule_id AS BLOB)) BETWEEN 1 AND 64),
		created_at DATETIME NOT NULL, created_by TEXT NOT NULL,
		PRIMARY KEY(dashboard_id, panel_id, rule_id))`,
	`CREATE INDEX IF NOT EXISTS idx_dashboard_panel_alerts_rule ON dashboard_panel_alerts(rule_id)`,
}
