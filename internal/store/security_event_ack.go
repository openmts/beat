package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrSecurityEventAcknowledged 表示事件已经被确认，重复确认不覆盖首个操作者。
var ErrSecurityEventAcknowledged = errors.New("store: security event is already acknowledged")

const securityEventNoteLimit = 500

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// SecurityEventAck 记录一次确认元数据，并与审计事件原子提交。
type SecurityEventAck struct {
	EventID string
	ActorID string
	Note    string
	At      time.Time
	Audit   model.AdminAuditEvent
}

func (store *AdminStore) AcknowledgeSecurityEvent(
	ctx context.Context, change SecurityEventAck,
) (model.SecurityEvent, error) {
	if err := validateSecurityEventAck(change); err != nil {
		return model.SecurityEvent{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := acknowledgeSecurityEventTx(ctx, tx, change); err != nil {
		return model.SecurityEvent{}, err
	}
	if err := insertAuditEvent(ctx, tx, &change.Audit); err != nil {
		return model.SecurityEvent{}, err
	}
	event, err := readSecurityEvent(ctx, tx, change.EventID)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.SecurityEvent{}, err
	}
	return event, nil
}

func acknowledgeSecurityEventTx(ctx context.Context, tx *sql.Tx, change SecurityEventAck) (int64, error) {
	result, err := tx.ExecContext(ctx, `UPDATE security_events SET acknowledged_at=?, acknowledged_by=?, acknowledged_note=?
		WHERE id=? AND acknowledged_at IS NULL`, change.At.UnixMilli(), change.ActorID, change.Note, change.EventID)
	if err != nil {
		return 0, fmt.Errorf("acknowledge security event: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("acknowledge security event rows: %w", err)
	}
	if affected == 1 {
		return affected, nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM security_events WHERE id=?", change.EventID).Scan(&exists); err != nil {
		return 0, fmt.Errorf("recheck security event: %w", err)
	}
	if exists == 0 {
		return 0, ErrSecurityEventNotFound
	}
	return 0, ErrSecurityEventAcknowledged
}

func validateSecurityEventAck(change SecurityEventAck) error {
	if change.EventID == "" || change.ActorID == "" || change.At.IsZero() || len(change.Note) > securityEventNoteLimit {
		return ErrSecurityEventInvalid
	}
	if change.Audit.ID == "" || change.Audit.RequestID == "" || change.Audit.CreatedAt.IsZero() {
		return ErrSecurityEventInvalid
	}
	return nil
}

func readSecurityEvent(ctx context.Context, query rowQuerier, id string) (model.SecurityEvent, error) {
	event, err := scanSecurityEvent(query.QueryRowContext(ctx, "SELECT "+securityEventColumns+
		" FROM security_events e"+securityEventDeliveryJoin+" WHERE e.id = ?", id))
	if err != nil {
		return model.SecurityEvent{}, fmt.Errorf("read security event: %w", err)
	}
	deliveries, err := securityEventDeliveries(ctx, query, event.MessageID)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	event.Deliveries = deliveries
	return event, nil
}
