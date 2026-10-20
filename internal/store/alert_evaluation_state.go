package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

type AlertEvaluationStore struct{ db *sql.DB }

type AlertEvaluationKey struct{ RuleID, NodeID, DeviceID string }

func NewAlertEvaluationStore(db *sql.DB) *AlertEvaluationStore { return &AlertEvaluationStore{db: db} }

const evaluationStateColumns = `rule_id,node_id,device_id,rule_revision,state_version,phase,evaluation_status,reason_code,
	trigger_elapsed_ms,recovery_elapsed_ms,pending_since,recovery_since,last_evaluated_at,last_good_at,
	last_evidence_at_ns,last_evidence_observed_at,last_evidence_id,last_run_id,active_event_id,active_event_status,
	health_epoch_id,health_status,health_started_at,health_status_since,health_last_observed_at,health_elapsed_ms,
	health_seen_mask,health_notified_mask,health_suspended,next_due_at,updated_at`

func (s *AlertEvaluationStore) Get(ctx context.Context, key AlertEvaluationKey) (*model.AlertEvaluationState, error) {
	return findEvaluationState(ctx, s.db, key)
}

func findEvaluationState(ctx context.Context, db schemaExecutor, key AlertEvaluationKey) (*model.AlertEvaluationState, error) {
	state, err := scanEvaluationState(db.QueryRowContext(ctx, "SELECT "+evaluationStateReadColumns+" FROM alert_evaluation_states WHERE rule_id=? AND node_id=? AND device_id=?", key.RuleID, key.NodeID, key.DeviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return state, err
}

func scanEvaluationState(row metricDefinitionScanner) (*model.AlertEvaluationState, error) {
	var state model.AlertEvaluationState
	var pending, recovery, evaluated, good, source, observed, healthStarted, healthSince, healthObserved sql.NullInt64
	var due, updated int64
	err := row.Scan(&state.RuleID, &state.NodeID, &state.DeviceID, &state.RuleRevision, &state.Version, &state.Phase, &state.Status, &state.Reason,
		&state.TriggerElapsedMS, &state.RecoveryElapsedMS, &pending, &recovery, &evaluated, &good, &source, &observed, &state.EvidenceID, &state.RunID, &state.ActiveEventID, &state.ActiveEventStatus,
		&state.Health.EpochID, &state.Health.Status, &healthStarted, &healthSince, &healthObserved, &state.Health.ElapsedMS, &state.Health.SeenMask, &state.Health.NotifiedMask, &state.Health.Suspended, &due, &updated, &state.DeviceName)
	if err != nil {
		return nil, err
	}
	state.PendingSince, state.RecoverySince = nullableMillis(pending), nullableMillis(recovery)
	state.LastEvaluatedAt, state.LastGoodAt = nullableMillis(evaluated), nullableMillis(good)
	state.LastEvidenceObservedAt = nullableMillis(observed)
	if source.Valid {
		at := time.Unix(0, source.Int64).UTC()
		state.LastEvidenceAt = &at
	}
	state.Health.StartedAt, state.Health.StatusSince, state.Health.LastObservedAt = nullableMillis(healthStarted), nullableMillis(healthSince), nullableMillis(healthObserved)
	state.NextDueAt, state.UpdatedAt = time.UnixMilli(due).UTC(), time.UnixMilli(updated).UTC()
	return &state, nil
}

func nullableMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	at := time.UnixMilli(value.Int64).UTC()
	return &at
}

func timeMillis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMilli()
}

func evaluationStateValues(state model.AlertEvaluationState) []any {
	var source any
	if state.LastEvidenceAt != nil {
		source = state.LastEvidenceAt.UnixNano()
	}
	return []any{state.RuleID, state.NodeID, state.DeviceID, state.RuleRevision, state.Version, state.Phase, state.Status, state.Reason,
		state.TriggerElapsedMS, state.RecoveryElapsedMS, timeMillis(state.PendingSince), timeMillis(state.RecoverySince), timeMillis(state.LastEvaluatedAt), timeMillis(state.LastGoodAt),
		source, timeMillis(state.LastEvidenceObservedAt), state.EvidenceID, state.RunID, state.ActiveEventID, state.ActiveEventStatus,
		state.Health.EpochID, state.Health.Status, timeMillis(state.Health.StartedAt), timeMillis(state.Health.StatusSince), timeMillis(state.Health.LastObservedAt), state.Health.ElapsedMS,
		state.Health.SeenMask, state.Health.NotifiedMask, state.Health.Suspended, state.NextDueAt.UnixMilli(), state.UpdatedAt.UnixMilli()}
}

const evaluationStateReadColumns = evaluationStateColumns + `,COALESCE((SELECT 'GPU '||ordinal||' · '||model FROM node_accelerators a WHERE a.node_id=alert_evaluation_states.node_id AND a.device_id=alert_evaluation_states.device_id),'')`
