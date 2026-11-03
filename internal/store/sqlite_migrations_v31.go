package store

import (
	"context"
	"fmt"
)

// applyEgressProxySettings 建立服务端后台 HTTP 出站代理配置。
// 代理 URL、用户名和密码作为一个加密载荷保存，接口不会返回明文密码。
func applyEgressProxySettings(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`ALTER TABLE application_secret_state RENAME TO application_secret_state_v30`,
		`CREATE TABLE application_secret_state (
 id INTEGER PRIMARY KEY CHECK(id=1),phase TEXT NOT NULL CHECK(phase IN('pending','backfill','sanitize','ready','rotating')),
 generation INTEGER NOT NULL CHECK(generation>=0),root_generation INTEGER NOT NULL CHECK(root_generation>=0),
 root_fingerprint TEXT NOT NULL,active_key_id TEXT NOT NULL,cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 16),
 cursor_id TEXT NOT NULL,rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id='' OR length(rotation_id)=36),updated_at INTEGER NOT NULL)`,
		`INSERT INTO application_secret_state SELECT * FROM application_secret_state_v30`,
		`DROP TABLE application_secret_state_v30`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("expand application secret resource cursor: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE egress_proxy_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0, 1)),
		proxy_secret_encrypted BLOB,
		revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
		updated_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("create egress proxy settings: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO egress_proxy_settings(id, enabled, revision, updated_at)
		VALUES(1, 0, 1, CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("seed egress proxy settings: %w", err)
	}
	return nil
}
