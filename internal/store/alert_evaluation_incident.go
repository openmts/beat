package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

// 非通知进度同样核对事件状态，阻止并发ACK/手动恢复被旧计算覆盖。
func recheckEvaluationIncident(ctx context.Context, tx *sql.Tx, change AlertEvaluationCommit) error {
	state := change.Decision.State
	expectedID, expectedStatus := state.ActiveEventID, state.ActiveEventStatus
	for _, action := range change.Decision.Actions {
		switch action.Kind {
		case alerteval.ActionThresholdTrigger:
			expectedID, expectedStatus = "", ""
		case alerteval.ActionDeviceUnavailable:
			if action.Event == nil || (action.PreviousStatus != model.AlertStatusTriggered && action.PreviousStatus != model.AlertStatusAcknowledged) {
				return alerteval.ErrConflict
			}
			expectedID, expectedStatus = action.Event.ID, action.PreviousStatus
		case alerteval.ActionThresholdRecovery:
			if action.Event == nil {
				return alerteval.ErrConflict
			}
			expectedID, expectedStatus = action.Event.ID, model.AlertStatusTriggered
		}
	}
	var id string
	var status model.AlertStatus
	err := tx.QueryRowContext(ctx, `SELECT id,status FROM alert_events WHERE rule_id=? AND node_id=? AND device_id=? AND status IN ('triggered','acknowledged')`, state.RuleID, state.NodeID, state.DeviceID).Scan(&id, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if id != expectedID || status != expectedStatus {
		return alerteval.ErrConflict
	}
	return nil
}
