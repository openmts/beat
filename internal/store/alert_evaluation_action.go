package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type evaluationAction struct {
	action alerteval.Action
	state  model.AlertEvaluationState
}

func applyEvaluationAction(ctx context.Context, tx *sql.Tx, input evaluationAction) error {
	if event := input.action.Event; event != nil && (event.RuleID != input.state.RuleID || event.NodeID != input.state.NodeID || event.DeviceID != input.state.DeviceID) {
		return alerteval.ErrConflict
	}
	switch input.action.Kind {
	case alerteval.ActionThresholdTrigger:
		if input.action.Event == nil {
			return alerteval.ErrConflict
		}
		changed, err := (&AlertEventStore{}).CreateActiveEventTx(ctx, tx, input.action.Event)
		if err != nil {
			return err
		}
		if !changed {
			return alerteval.ErrConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE alert_events SET rule_revision=? WHERE id=?", input.state.RuleRevision, input.action.Event.ID)
		return err
	case alerteval.ActionDeviceUnavailable:
		event := input.action.Event
		if event == nil || event.DeviceID == "" || event.ResolutionReason != model.AcceleratorDeviceUnavailable {
			return alerteval.ErrConflict
		}
		changed, err := (&AlertEventStore{}).TransitionEventTx(ctx, tx, AlertEventTransition{Event: *event, Previous: input.action.PreviousStatus})
		if err != nil {
			return err
		}
		if !changed {
			return alerteval.ErrConflict
		}
		return nil
	case alerteval.ActionThresholdRecovery:
		return applyThresholdRecovery(ctx, tx, input.action)
	case alerteval.ActionHealth:
		return writeEvaluationHealthEvent(ctx, tx, input)
	default:
		return alerteval.ErrConflict
	}
}

func applyThresholdRecovery(ctx context.Context, tx *sql.Tx, action alerteval.Action) error {
	if action.Event == nil {
		return alerteval.ErrConflict
	}
	var status model.AlertStatus
	if err := tx.QueryRowContext(ctx, "SELECT status FROM alert_events WHERE id=?", action.Event.ID).Scan(&status); err != nil {
		return err
	}
	if status != model.AlertStatusTriggered {
		return alerteval.ErrConflict
	}
	changed, err := (&AlertEventStore{}).TransitionEventTx(ctx, tx, AlertEventTransition{Event: *action.Event, Previous: status})
	if err != nil {
		return err
	}
	if !changed {
		return alerteval.ErrConflict
	}
	return nil
}

func writeEvaluationHealthEvent(ctx context.Context, tx *sql.Tx, input evaluationAction) error {
	event := input.action.Health
	if event == nil || event.RuleID != input.state.RuleID || event.NodeID != input.state.NodeID || event.DeviceID != input.state.DeviceID {
		return alerteval.ErrConflict
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO alert_evaluation_events(id,rule_id,node_id,rule_revision,epoch_id,kind,reason_code,occurred_at,notified_at,closed_at,source_key,device_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(epoch_id,kind) DO UPDATE SET notified_at=COALESCE(alert_evaluation_events.notified_at,excluded.notified_at),closed_at=excluded.closed_at`,
		event.ID, event.RuleID, event.NodeID, event.RuleRevision, event.EpochID, event.Kind, event.Reason, event.OccurredAt.UnixMilli(), timeMillis(event.NotifiedAt), timeMillis(event.ClosedAt), event.SourceKey, event.DeviceID)
	if err != nil {
		return err
	}
	if event.Kind == "recovered" {
		if _, err := tx.ExecContext(ctx, "UPDATE alert_evaluation_events SET closed_at=? WHERE epoch_id=?", event.OccurredAt.UnixMilli(), event.EpochID); err != nil {
			return err
		}
	}
	audit := model.AdminAuditEvent{ID: uuid.NewString(), ActorUsername: "system", Action: "alert_evaluation." + event.Kind, ResourceType: "alert_evaluation", ResourceID: event.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", CreatedAt: model.NowUTC()}
	return insertAuditEvent(ctx, tx, &audit)
}
