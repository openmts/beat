package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrSecurityEventSourceUnavailable 表示事件没有可揭示的受保护来源记录。
var ErrSecurityEventSourceUnavailable = errors.New("store: security event has no protected source record")

// SecurityEventSourceReveal 记录一次来源揭示请求，并与审计事件原子提交。
type SecurityEventSourceReveal struct {
	EventID string
	ActorID string
	At      time.Time
	Audit   model.AdminAuditEvent
}

// RevealSecurityEventSource 返回事件关联审计记录里的规范来源地址。
// 事件表只保留截断前缀，完整地址只存在于受保护的审计数据中。
func (store *AdminStore) RevealSecurityEventSource(
	ctx context.Context, reveal SecurityEventSourceReveal,
) (model.SecurityEventSource, error) {
	if reveal.EventID == "" || reveal.ActorID == "" || reveal.Audit.ID == "" || reveal.At.IsZero() {
		return model.SecurityEventSource{}, ErrSecurityEventSourceUnavailable
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SecurityEventSource{}, fmt.Errorf("begin security source reveal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	source, err := securityEventSourceTx(ctx, tx, reveal.EventID)
	if err != nil {
		return model.SecurityEventSource{}, err
	}
	if err := insertAuditEvent(ctx, tx, &reveal.Audit); err != nil {
		return model.SecurityEventSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.SecurityEventSource{}, fmt.Errorf("commit security source reveal: %w", err)
	}
	return source, nil
}

func securityEventSourceTx(ctx context.Context, query rowQuerier, eventID string) (model.SecurityEventSource, error) {
	var source model.SecurityEventSource
	err := query.QueryRowContext(ctx, `SELECT a.id, a.ip_address, a.created_at
		FROM security_events e JOIN admin_audit_events a ON a.id = e.audit_event_id
		WHERE e.id = ?`, eventID).Scan(&source.EventID, &source.Address, &source.CapturedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SecurityEventSource{}, securityEventSourceMiss(ctx, query, eventID)
	}
	if err != nil {
		return model.SecurityEventSource{}, fmt.Errorf("read protected security source: %w", err)
	}
	if source.Address == "" {
		return model.SecurityEventSource{}, ErrSecurityEventSourceUnavailable
	}
	source.EventID = eventID
	return source, nil
}

// securityEventSourceMiss 区分“事件不存在”与“事件缺少受保护来源记录”，
// 让接口分别返回 404 与 409，而不是把删除后的事件当成受保护记录缺失。
func securityEventSourceMiss(ctx context.Context, query rowQuerier, eventID string) error {
	var present int
	err := query.QueryRowContext(ctx, `SELECT 1 FROM security_events WHERE id = ?`, eventID).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSecurityEventNotFound
	}
	if err != nil {
		return fmt.Errorf("read security event for reveal: %w", err)
	}
	return ErrSecurityEventSourceUnavailable
}
