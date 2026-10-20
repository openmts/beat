package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// auditActorType 归一化主体类型：旧调用方未显式设置时按浏览器管理员记录。
func auditActorType(value string) string {
	if value == model.AuditActorService {
		return model.AuditActorService
	}
	return model.AuditActorAdmin
}

// insertAuditEvent 可与应用变更共享事务，避免成功响应但审计落盘失败。
func insertAuditEvent(ctx context.Context, database schemaExecutor, event *model.AdminAuditEvent) error {
	result, err := database.ExecContext(ctx, `INSERT INTO admin_audit_events (
		id, request_id, actor_id, actor_username, action, resource_type, resource_id,
		outcome, detail_json, ip_address, user_agent, session_prefix, created_at, actor_type
	) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.RequestID,
		event.ActorID, event.ActorUsername, event.Action, event.ResourceType, event.ResourceID,
		event.Outcome, event.DetailJSON, event.IPAddress, event.UserAgent, event.SessionPrefix,
		event.CreatedAt, auditActorType(event.ActorType))
	if err != nil {
		return fmt.Errorf("create administrator audit event: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("administrator audit event was not recorded: %w", err)
	}
	if count != 1 {
		return errors.New("administrator audit event was not recorded")
	}
	return nil
}
