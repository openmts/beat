package store

import "context"

// applyAgentReleaseRetention 建发布对象保留期的后台设置表。
// 单行表（id=1）：Configured=0 表示仍用启动环境变量缺省，因此升级后行为不变。
func applyAgentReleaseRetention(ctx context.Context, db schemaExecutor) error {
	statement := `CREATE TABLE agent_release_retention(
		id INTEGER PRIMARY KEY CHECK(id = 1),
		configured INTEGER NOT NULL DEFAULT 0 CHECK(configured IN (0, 1)),
		rollout_days INTEGER NOT NULL DEFAULT 0 CHECK(rollout_days BETWEEN 0 AND 3650),
		release_days INTEGER NOT NULL DEFAULT 0 CHECK(release_days BETWEEN 0 AND 3650),
		last_run_at DATETIME,
		last_status TEXT NOT NULL DEFAULT 'idle',
		last_error TEXT NOT NULL DEFAULT '',
		last_rollouts INTEGER NOT NULL DEFAULT 0 CHECK(last_rollouts >= 0),
		last_releases INTEGER NOT NULL DEFAULT 0 CHECK(last_releases >= 0),
		last_artifacts INTEGER NOT NULL DEFAULT 0 CHECK(last_artifacts >= 0),
		last_trigger TEXT NOT NULL DEFAULT '',
		updated_at DATETIME NOT NULL
	)`
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO agent_release_retention(id, configured, rollout_days, release_days, updated_at)
		VALUES(1, 0, 0, 0, CURRENT_TIMESTAMP)`)
	return err
}
