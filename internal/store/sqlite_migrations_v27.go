package store

import "context"

// applyDashboardVersions 引入 v27 看板版本历史：为每块看板保留最近 20 个版本的元数据与定义，
// 并把现存看板的当前定义与 previous_definition_json 回填成历史记录（迁移前没有更早的线索，
// 不凭空造版本）。
func applyDashboardVersions(ctx context.Context, db schemaExecutor) error {
	for _, statement := range dashboardVersionSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return backfillDashboardVersions(ctx, db)
}

var dashboardVersionSchema = []string{
	`CREATE TABLE IF NOT EXISTS dashboard_versions (
		dashboard_id TEXT NOT NULL CHECK(length(CAST(dashboard_id AS BLOB)) BETWEEN 1 AND 80),
		revision INTEGER NOT NULL CHECK(revision > 0),
		title TEXT NOT NULL DEFAULT '' CHECK(length(CAST(title AS BLOB)) <= 200),
		summary TEXT NOT NULL DEFAULT '' CHECK(length(CAST(summary AS BLOB)) <= 200),
		definition_json TEXT NOT NULL CHECK(json_valid(definition_json)
			AND length(CAST(definition_json AS BLOB)) <= 65536),
		created_at DATETIME NOT NULL, created_by TEXT NOT NULL,
		PRIMARY KEY (dashboard_id, revision))`,
	`CREATE INDEX IF NOT EXISTS idx_dashboard_versions_recent
		ON dashboard_versions(dashboard_id, revision DESC)`,
}

func backfillDashboardVersions(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`INSERT OR IGNORE INTO dashboard_versions
			(dashboard_id, revision, title, summary, definition_json, created_at, created_by)
			SELECT id, revision, COALESCE(json_extract(definition_json, '$.title'), ''), '',
				definition_json, updated_at, COALESCE(NULLIF(updated_by, ''), 'system')
			FROM metric_dashboards`,
		`INSERT OR IGNORE INTO dashboard_versions
			(dashboard_id, revision, title, summary, definition_json, created_at, created_by)
			SELECT id, revision - 1, COALESCE(json_extract(previous_definition_json, '$.title'), ''), '',
				previous_definition_json, updated_at, COALESCE(NULLIF(updated_by, ''), 'system')
			FROM metric_dashboards WHERE previous_definition_json IS NOT NULL AND revision > 1`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
