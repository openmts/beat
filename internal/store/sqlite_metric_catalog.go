package store

import "context"

func applyMetricCatalog(ctx context.Context, db schemaExecutor) error {
	for _, statement := range metricCatalogSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return reconcileMetricCatalog(ctx, db)
}

var metricCatalogSchema = []string{
	`CREATE TABLE IF NOT EXISTS metric_catalog_state (
		id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL CHECK(version>0),
		revision INTEGER NOT NULL CHECK(revision>0))`,
	`INSERT OR IGNORE INTO metric_catalog_state(id,version,revision) VALUES(1,1,1)`,
	`CREATE TABLE IF NOT EXISTS metric_definitions (
		metric_key TEXT PRIMARY KEY CHECK(length(CAST(metric_key AS BLOB)) BETWEEN 1 AND 107),
		label TEXT NOT NULL CHECK(length(CAST(label AS BLOB)) BETWEEN 1 AND 128),
		description TEXT NOT NULL DEFAULT '' CHECK(length(CAST(description AS BLOB))<=2048),
		source TEXT NOT NULL CHECK(source IN ('builtin','derived','custom')),
		kind TEXT NOT NULL CHECK(kind IN ('gauge','counter')), unit TEXT NOT NULL,
		aggregations_json TEXT NOT NULL CHECK(json_valid(aggregations_json) AND length(aggregations_json)<=256),
		default_aggregation TEXT NOT NULL, minimum REAL, maximum REAL,
		tags_json TEXT NOT NULL CHECK(json_valid(tags_json) AND length(tags_json)<=256),
		public INTEGER NOT NULL CHECK(public IN (0,1)), enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
		retention_days INTEGER CHECK(retention_days BETWEEN 1 AND 3650),
		display_order INTEGER NOT NULL DEFAULT 0 CHECK(display_order>=0), revision INTEGER NOT NULL CHECK(revision>0),
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		created_by TEXT NOT NULL DEFAULT 'system', updated_by TEXT NOT NULL DEFAULT 'system',
		CHECK(minimum IS NULL OR maximum IS NULL OR minimum<=maximum))`,
	`CREATE TRIGGER IF NOT EXISTS metric_definition_created AFTER INSERT ON metric_definitions
		BEGIN UPDATE metric_catalog_state SET revision=revision+1 WHERE id=1; END`,
	`CREATE TRIGGER IF NOT EXISTS metric_definition_revised AFTER UPDATE ON metric_definitions
		BEGIN UPDATE metric_catalog_state SET revision=revision+1 WHERE id=1; END`,
	`CREATE TABLE IF NOT EXISTS metric_retention_jobs (
		metric_key TEXT PRIMARY KEY REFERENCES metric_definitions(metric_key), revision INTEGER NOT NULL CHECK(revision>0),
		state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','complete')),
		requested_at DATETIME NOT NULL, started_at DATETIME, completed_at DATETIME, next_attempt_at DATETIME,
		attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0), last_error TEXT NOT NULL DEFAULT '' CHECK(length(last_error)<=1024))`,
	`CREATE TABLE IF NOT EXISTS metric_dashboards (
		id TEXT PRIMARY KEY, scope TEXT NOT NULL CHECK(scope IN ('personal','public','admin','node')),
		owner_user_id TEXT REFERENCES admin_users(id) ON DELETE CASCADE,
		node_id TEXT REFERENCES nodes(id) ON DELETE CASCADE, revision INTEGER NOT NULL CHECK(revision>0),
		definition_json TEXT NOT NULL CHECK(json_valid(definition_json) AND length(CAST(definition_json AS BLOB))<=65536),
		previous_definition_json TEXT CHECK(previous_definition_json IS NULL OR
			(json_valid(previous_definition_json) AND length(CAST(previous_definition_json AS BLOB))<=65536)),
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, updated_by TEXT NOT NULL,
		CHECK((scope='personal' AND owner_user_id IS NOT NULL) OR (scope<>'personal' AND owner_user_id IS NULL)),
		CHECK((scope='node' AND node_id IS NOT NULL) OR (scope<>'node' AND node_id IS NULL)))`,
}
