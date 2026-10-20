package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

const alertEventColumns = "id,rule_id,node_id,message,value,status,triggered_at,acknowledged_at,resolved_at,device_id,device_name,resolution_reason,rule_name_snapshot,node_name_snapshot"

func scanAlertEvent(row metricDefinitionScanner) (*model.AlertEvent, error) {
	var event model.AlertEvent
	var acknowledged, resolved sql.NullTime
	err := row.Scan(&event.ID, &event.RuleID, &event.NodeID, &event.Message, &event.Value, &event.Status, &event.TriggeredAt, &acknowledged, &resolved, &event.DeviceID, &event.DeviceName, &event.ResolutionReason, &event.RuleNameSnapshot, &event.NodeNameSnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if acknowledged.Valid {
		event.AcknowledgedAt = &acknowledged.Time
	}
	if resolved.Valid {
		event.ResolvedAt = &resolved.Time
	}
	return &event, nil
}

func (s *AlertEventStore) GetActiveEventForKey(ctx context.Context, key AlertEvaluationKey) (*model.AlertEvent, error) {
	return scanAlertEvent(s.db.QueryRowContext(ctx, "SELECT "+alertEventColumns+" FROM alert_events WHERE rule_id=? AND node_id=? AND device_id=? AND status IN ('triggered','acknowledged') LIMIT 1", key.RuleID, key.NodeID, key.DeviceID))
}
