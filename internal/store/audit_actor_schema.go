package store

import (
	"context"
	"fmt"
)

// auditActorColumn 由 v16 服务主体迁移幂等补齐：审计表早期版本只记录浏览器主体。
const auditActorColumn = "actor_type"

// ensureAuditActorColumn 为旧库补 actor_type，新库在建表时已包含该列。
func ensureAuditActorColumn(ctx context.Context, db schemaExecutor) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(admin_audit_events)")
	if err != nil {
		return fmt.Errorf("query audit columns: %w", err)
	}
	exists := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan audit column: %w", err)
		}
		if name == auditActorColumn {
			exists = true
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close audit columns: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate audit columns: %w", err)
	}
	if exists {
		return nil
	}
	statement := fmt.Sprintf("ALTER TABLE admin_audit_events ADD COLUMN %s TEXT NOT NULL DEFAULT 'admin'", auditActorColumn)
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("add audit column %s: %w", auditActorColumn, err)
	}
	return nil
}
