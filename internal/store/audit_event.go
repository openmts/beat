package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrInvalidAuditEventQuery 表示游标、过滤器或 limit 不合法；调用方应返回 400。
var ErrInvalidAuditEventQuery = errors.New("invalid audit event query")

const (
	// AuditDefaultPage 与 AuditMaximumPage 约束审计历史的每页上限。
	AuditDefaultPage = 50
	AuditMaximumPage = 200

	auditMaximumCursorBytes = 512
	auditCursorVersion      = 1
)

const auditColumns = `id, request_id, COALESCE(actor_id, ''), actor_username, action,
	resource_type, resource_id, outcome, detail_json, ip_address, user_agent, session_prefix, created_at,
	actor_type`

// auditCursor 绑定归一化过滤器；跨过滤复用或版本不符一律拒绝。
type auditCursor struct {
	Action  string `json:"action,omitempty"`
	ActorID string `json:"actor,omitempty"`
	At      int64  `json:"at"`
	ID      string `json:"id"`
	Version int    `json:"v"`
}

func (store *AdminStore) CreateAuditEvent(ctx context.Context, event *model.AdminAuditEvent) error {
	return insertAuditEvent(ctx, store.db, event)
}

// ListAuditEvents 用不透明的 (created_at,id) 游标分页，不再暴露 offset 或总数。
func (store *AdminStore) ListAuditEvents(
	ctx context.Context,
	filter model.AuditFilter,
) (model.AuditPage, error) {
	statement, args, limit, err := auditEventQuery(filter)
	if err != nil {
		return model.AuditPage{}, err
	}
	rows, err := store.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return model.AuditPage{}, fmt.Errorf("list administrator audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := model.AuditPage{Events: []model.AdminAuditEvent{}}
	for rows.Next() {
		var event model.AdminAuditEvent
		if err := rows.Scan(&event.ID, &event.RequestID, &event.ActorID, &event.ActorUsername,
			&event.Action, &event.ResourceType, &event.ResourceID, &event.Outcome, &event.DetailJSON,
			&event.IPAddress, &event.UserAgent, &event.SessionPrefix, &event.CreatedAt, &event.ActorType); err != nil {
			return model.AuditPage{}, fmt.Errorf("scan administrator audit event: %w", err)
		}
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return model.AuditPage{}, fmt.Errorf("iterate administrator audit events: %w", err)
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor, err = encodeAuditCursor(filter, last)
	}
	return page, err
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

func auditEventQuery(filter model.AuditFilter) (string, []any, int, error) {
	limit, err := auditQueryLimit(filter.Limit)
	if err != nil {
		return "", nil, 0, err
	}
	cursor, err := decodeAuditCursor(filter.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	filters, args := []string{"1=1"}, []any{}
	for _, item := range []struct{ column, value string }{{"action", filter.Action}, {"actor_id", filter.ActorID}} {
		if item.value != "" {
			filters = append(filters, item.column+"=?")
			args = append(args, item.value)
		}
	}
	if filter.Cursor != "" {
		if cursor.Action != filter.Action || cursor.ActorID != filter.ActorID {
			return "", nil, 0, ErrInvalidAuditEventQuery
		}
		filters = append(filters, "(created_at<? OR (created_at=? AND id<?))")
		at := time.UnixMilli(cursor.At).UTC()
		args = append(args, at, at, cursor.ID)
	}
	statement := "SELECT " + auditColumns + " FROM admin_audit_events WHERE " +
		strings.Join(filters, " AND ") + " ORDER BY created_at DESC,id DESC LIMIT ?"
	return statement, append(args, limit+1), limit, nil
}

func auditQueryLimit(limit int) (int, error) {
	if limit == 0 {
		return AuditDefaultPage, nil
	}
	if limit < 1 || limit > AuditMaximumPage {
		return 0, ErrInvalidAuditEventQuery
	}
	return limit, nil
}

func encodeAuditCursor(filter model.AuditFilter, last model.AdminAuditEvent) (string, error) {
	data, err := json.Marshal(auditCursor{
		Action: filter.Action, ActorID: filter.ActorID,
		At: last.CreatedAt.UnixMilli(), ID: last.ID, Version: auditCursorVersion,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeAuditCursor(encoded string) (auditCursor, error) {
	var cursor auditCursor
	if encoded == "" {
		return cursor, nil
	}
	if len(encoded) > auditMaximumCursorBytes {
		return cursor, ErrInvalidAuditEventQuery
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, ErrInvalidAuditEventQuery
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, ErrInvalidAuditEventQuery
	}
	if cursor.Version != auditCursorVersion || cursor.ID == "" || len(cursor.ID) > 128 {
		return cursor, ErrInvalidAuditEventQuery
	}
	return cursor, nil
}
