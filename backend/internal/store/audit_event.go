package store

import (
	"context"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const auditColumns = `id, request_id, COALESCE(actor_id, ''), actor_username, action,
	resource_type, resource_id, outcome, detail_json, ip_address, user_agent, session_prefix, created_at`

func (store *AdminStore) CreateAuditEvent(ctx context.Context, event *model.AdminAuditEvent) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_audit_events (
		id, request_id, actor_id, actor_username, action, resource_type, resource_id,
		outcome, detail_json, ip_address, user_agent, session_prefix, created_at
	) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.RequestID,
		event.ActorID, event.ActorUsername, event.Action, event.ResourceType, event.ResourceID,
		event.Outcome, event.DetailJSON, event.IPAddress, event.UserAgent, event.SessionPrefix, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("create administrator audit event: %w", err)
	}
	return nil
}

func (store *AdminStore) ListAuditEvents(ctx context.Context, filter model.AuditFilter) (model.AuditPage, error) {
	filter = normalizeAuditFilter(filter)
	where, args := auditWhere(filter)
	var total int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_audit_events"+where, args...).Scan(&total); err != nil {
		return model.AuditPage{}, fmt.Errorf("count administrator audit events: %w", err)
	}
	queryArgs := append(args, filter.Limit, filter.Offset)
	rows, err := store.db.QueryContext(ctx, "SELECT "+auditColumns+" FROM admin_audit_events"+
		where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return model.AuditPage{}, fmt.Errorf("list administrator audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []model.AdminAuditEvent{}
	for rows.Next() {
		var event model.AdminAuditEvent
		if err := rows.Scan(&event.ID, &event.RequestID, &event.ActorID, &event.ActorUsername,
			&event.Action, &event.ResourceType, &event.ResourceID, &event.Outcome, &event.DetailJSON,
			&event.IPAddress, &event.UserAgent, &event.SessionPrefix, &event.CreatedAt); err != nil {
			return model.AuditPage{}, fmt.Errorf("scan administrator audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return model.AuditPage{}, fmt.Errorf("iterate administrator audit events: %w", err)
	}
	return model.AuditPage{Events: events, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (store *AdminStore) CleanupAuditEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx, "DELETE FROM admin_audit_events WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup administrator audit events: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cleanup administrator audit events rows affected: %w", err)
	}
	return count, nil
}

func normalizeAuditFilter(filter model.AuditFilter) model.AuditFilter {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func auditWhere(filter model.AuditFilter) (string, []any) {
	if filter.Action != "" && filter.ActorID != "" {
		return " WHERE action = ? AND actor_id = ?", []any{filter.Action, filter.ActorID}
	}
	if filter.Action != "" {
		return " WHERE action = ?", []any{filter.Action}
	}
	if filter.ActorID != "" {
		return " WHERE actor_id = ?", []any{filter.ActorID}
	}
	return "", nil
}
