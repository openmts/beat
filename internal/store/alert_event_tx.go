package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var ErrInvalidAlertTransition = errors.New("invalid alert event transition")

type AlertEventTransition struct {
	Event    model.AlertEvent
	Previous model.AlertStatus
	Actor    *EventMutationActor
}

type EventMutationActor struct {
	UserID, SessionID, RequestID, IPAddress, UserAgent string
}

// CreateActiveEventTx 与通知入队共享调用方的事务，已存在活动事件时不重复创建。
func (s *AlertEventStore) CreateActiveEventTx(ctx context.Context, transaction *sql.Tx, event *model.AlertEvent) (bool, error) {
	if transaction == nil || event == nil {
		return false, ErrInvalidAlertTransition
	}
	if err := validateNewAlertEvent(*event); err != nil {
		return false, err
	}
	var exists bool
	if err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_events WHERE rule_id=? AND node_id=? AND device_id=? AND status IN ('triggered','acknowledged'))`, event.RuleID, event.NodeID, event.DeviceID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO alert_events
		(id,rule_id,node_id,message,value,status,triggered_at,acknowledged_at,resolved_at,device_id,device_name,rule_name_snapshot,node_name_snapshot)
		VALUES(?,?,?,?,?,?,?,NULL,NULL,?,?,?,?)`,
		event.ID, event.RuleID, event.NodeID, event.Message, event.Value, event.Status, event.TriggeredAt, event.DeviceID, event.DeviceName, event.RuleName, event.NodeName)
	inserted, err := eventMutationResult(result, err)
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, errors.New("alert event insertion was not persisted")
	}
	if err := recordAlertTransition(ctx, transaction, AlertEventTransition{Event: *event}); err != nil {
		return false, err
	}
	return true, nil
}

// TransitionEventTx 用旧状态比较更新，避免人工确认和自动恢复互相覆盖。
func (s *AlertEventStore) TransitionEventTx(ctx context.Context, transaction *sql.Tx, change AlertEventTransition) (bool, error) {
	if transaction == nil {
		return false, ErrInvalidAlertTransition
	}
	if err := validateAlertTransition(change); err != nil {
		return false, err
	}
	if change.Actor != nil {
		if err := validateEventActor(ctx, transaction, *change.Actor); err != nil {
			return false, err
		}
	}
	result, err := transaction.ExecContext(ctx, `UPDATE alert_events SET status=?,acknowledged_at=?,resolved_at=?,resolution_reason=?
		WHERE id=? AND rule_id=? AND node_id=? AND device_id=? AND status=?`, change.Event.Status, change.Event.AcknowledgedAt, change.Event.ResolvedAt, change.Event.ResolutionReason,
		change.Event.ID, change.Event.RuleID, change.Event.NodeID, change.Event.DeviceID, change.Previous)
	updated, err := eventMutationResult(result, err)
	if err != nil {
		return false, err
	}
	if !updated {
		return false, checkSkippedAlertTransition(ctx, transaction, change)
	}
	if err := recordAlertTransition(ctx, transaction, change); err != nil {
		return false, err
	}
	return true, nil
}

func checkSkippedAlertTransition(ctx context.Context, transaction *sql.Tx, change AlertEventTransition) error {
	var matches bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_events WHERE id=? AND rule_id=? AND node_id=? AND device_id=? AND status=?)`,
		change.Event.ID, change.Event.RuleID, change.Event.NodeID, change.Event.DeviceID, change.Previous).Scan(&matches)
	if err != nil {
		return err
	}
	if matches {
		return errors.New("alert transition was not persisted")
	}
	return nil
}

func validateNewAlertEvent(event model.AlertEvent) error {
	if event.DeviceID != "" && (!model.ValidAcceleratorDeviceID(event.DeviceID) || len(event.DeviceName) > 256) {
		return ErrInvalidAlertTransition
	}
	if event.ID == "" || event.RuleID == "" || event.NodeID == "" || event.TriggeredAt.IsZero() {
		return ErrInvalidAlertTransition
	}
	if event.Status != model.AlertStatusTriggered || math.IsNaN(event.Value) || math.IsInf(event.Value, 0) {
		return ErrInvalidAlertTransition
	}
	return nil
}

func validateAlertTransition(change AlertEventTransition) error {
	if !hasAlertEventIdentity(change.Event) {
		return ErrInvalidAlertTransition
	}
	if change.Event.Status == model.AlertStatusAcknowledged {
		if change.Previous != model.AlertStatusTriggered || change.Event.AcknowledgedAt == nil {
			return ErrInvalidAlertTransition
		}
		return nil
	}
	if change.Event.Status != model.AlertStatusResolved || change.Event.ResolvedAt == nil {
		return ErrInvalidAlertTransition
	}
	if change.Previous != model.AlertStatusTriggered && change.Previous != model.AlertStatusAcknowledged {
		return ErrInvalidAlertTransition
	}
	return nil
}

func hasAlertEventIdentity(event model.AlertEvent) bool {
	return event.ID != "" && event.RuleID != "" && event.NodeID != ""
}

func eventMutationResult(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count < 0 || count > 1 {
		return false, fmt.Errorf("alert mutation affected unexpected row count %d", count)
	}
	return count == 1, nil
}

func recordAlertTransition(ctx context.Context, transaction *sql.Tx, change AlertEventTransition) error {
	event := model.AdminAuditEvent{ID: uuid.NewString(), Action: "alert." + string(change.Event.Status), ResourceType: "alert_event", ResourceID: change.Event.ID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: `{}`, ActorUsername: "system", CreatedAt: model.NowUTC()}
	if change.Actor != nil {
		event.ActorID, event.RequestID = change.Actor.UserID, change.Actor.RequestID
		event.IPAddress, event.UserAgent = change.Actor.IPAddress, change.Actor.UserAgent
		err := transaction.QueryRowContext(ctx, `SELECT u.username,s.token_prefix FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id
			WHERE u.id=? AND s.id=?`, change.Actor.UserID, change.Actor.SessionID).Scan(&event.ActorUsername, &event.SessionPrefix)
		if err != nil {
			return err
		}
	}
	return insertAuditEvent(ctx, transaction, &event)
}

func validateEventActor(ctx context.Context, transaction *sql.Tx, actor EventMutationActor) error {
	var id string
	now := model.NowUTC()
	err := transaction.QueryRowContext(ctx, `SELECT s.id FROM admin_sessions s JOIN admin_users u ON s.user_id=u.id
		WHERE s.id=? AND s.user_id=? AND u.enabled=1 AND u.role IN ('owner','admin') AND s.revoked_at IS NULL
		AND s.idle_expires_at>? AND s.absolute_expires_at>?`, actor.SessionID, actor.UserID, now, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAdminSessionNotFound
	}
	return err
}
