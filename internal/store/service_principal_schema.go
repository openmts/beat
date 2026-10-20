package store

import "context"

// MigrateServicePrincipals 登记 v16 服务令牌主体、权限与资源约束表。
func MigrateServicePrincipals(ctx context.Context, db schemaExecutor) error {
	for _, statement := range servicePrincipalSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return ensureAuditActorColumn(ctx, db)
}

var servicePrincipalSchema = []string{
	`CREATE TABLE service_principals(
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 128),
		purpose TEXT NOT NULL DEFAULT '' CHECK(length(purpose) <= 512),
		token_prefix TEXT NOT NULL CHECK(length(token_prefix) BETWEEN 1 AND 64),
		token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
		visibility TEXT NOT NULL CHECK(visibility IN('public','private')),
		source_cidrs TEXT NOT NULL DEFAULT '[]',
		expires_at DATETIME NOT NULL,
		revoked_at DATETIME,
		revoked_reason TEXT NOT NULL DEFAULT '',
		superseded_at DATETIME,
		rotation_overlap_seconds INTEGER NOT NULL DEFAULT 0 CHECK(rotation_overlap_seconds BETWEEN 0 AND 86400),
		rotated_from TEXT REFERENCES service_principals(id),
		revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
		created_by TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		last_used_at DATETIME
	)`,
	`CREATE INDEX idx_service_principals_expiry ON service_principals(expires_at)`,
	`CREATE INDEX idx_service_principals_active ON service_principals(revoked_at, superseded_at)`,
	`CREATE TABLE service_token_scopes(
		principal_id TEXT NOT NULL REFERENCES service_principals(id) ON DELETE CASCADE,
		scope TEXT NOT NULL,
		PRIMARY KEY(principal_id, scope)
	)`,
	`CREATE TABLE service_token_resources(
		principal_id TEXT NOT NULL REFERENCES service_principals(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK(kind IN('node','group','channel')),
		resource_id TEXT NOT NULL CHECK(length(resource_id) BETWEEN 1 AND 128),
		PRIMARY KEY(principal_id, kind, resource_id)
	)`,
}
