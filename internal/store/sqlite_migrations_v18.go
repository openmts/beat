package store

import (
	"context"
	"fmt"
)

// MigrateBackupUpload 引入可分块续传的备份上传会话与分块账本。
//
// 会话本身不保存块字节，只保存声明信息、进度与私有暂存目录名；块字节由备份服务
// 写在 0700 暂存目录的 0600 文件里。v18 之前的保留迁移从未随任何发布分发。
func MigrateBackupUpload(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS backup_upload_sessions (
			id TEXT PRIMARY KEY,
			owner_user_id TEXT NOT NULL CHECK(length(owner_user_id) BETWEEN 1 AND 128),
			filename TEXT NOT NULL CHECK(length(filename) BETWEEN 1 AND 200),
			declared_bytes INTEGER NOT NULL CHECK(declared_bytes BETWEEN 0 AND 4294967296),
			declared_digest TEXT NOT NULL CHECK(length(declared_digest) = 64),
			chunk_bytes INTEGER NOT NULL CHECK(chunk_bytes BETWEEN 1048576 AND 67108864),
			chunk_count INTEGER NOT NULL CHECK(chunk_count BETWEEN 1 AND 4096),
			received_bytes INTEGER NOT NULL DEFAULT 0 CHECK(received_bytes >= 0),
			received_chunks INTEGER NOT NULL DEFAULT 0 CHECK(received_chunks >= 0),
			staging_name TEXT NOT NULL UNIQUE CHECK(length(staging_name) BETWEEN 1 AND 128),
			state TEXT NOT NULL CHECK(state IN ('open', 'quarantined', 'validated', 'cancelled', 'expired')),
			backup_id TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			staging_removed INTEGER NOT NULL DEFAULT 0 CHECK(staging_removed IN (0, 1)),
			expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_upload_sessions_owner
			ON backup_upload_sessions(owner_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_upload_sessions_expiry
			ON backup_upload_sessions(state, expires_at)`,
		`CREATE TABLE IF NOT EXISTS backup_upload_chunks (
			session_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL CHECK(chunk_index BETWEEN 0 AND 4095),
			offset_bytes INTEGER NOT NULL CHECK(offset_bytes >= 0),
			size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 67108864),
			digest TEXT NOT NULL CHECK(length(digest) = 64),
			created_at DATETIME NOT NULL,
			PRIMARY KEY (session_id, chunk_index)
		)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply resumable upload schema: %w", err)
		}
	}
	return nil
}
