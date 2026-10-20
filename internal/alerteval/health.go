package alerteval

import (
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const noDataMask = 1

const errorMask = 2

func unhealthyStep(rule model.AlertEvaluationRule, result StepResult, observation model.AlertEvaluationObservation) (StepResult, error) {
	state := &result.State
	resetConfirmation(state)
	state.Status, state.Reason = observation.Status, observation.Reason
	if state.Health.EpochID == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return StepResult{}, ErrUnavailable
		}
		state.Health = model.AlertEvaluationHealth{EpochID: id.String(), StartedAt: timePointer(observation.ObservedAt)}
	}
	health := &state.Health
	creditHealthDelay(state, observation)
	mask, policy, delay := healthPolicy(rule, observation.Status)
	first := health.SeenMask&mask == 0
	health.SeenMask |= mask
	notify := policy == model.EvaluationNotify && health.ElapsedMS >= int64(delay)*1000 && health.NotifiedMask&mask == 0
	if notify {
		health.NotifiedMask |= mask
	}
	if first || notify {
		event := healthEvent(*state, observation.Status, observation.Reason, observation.ObservedAt)
		if notify {
			event.NotifiedAt = timePointer(observation.ObservedAt)
		}
		result.Actions = append(result.Actions, Action{Kind: ActionHealth, Notify: notify, Health: &event})
	}
	state.RunID = observation.RunID
	return result, nil
}

func creditHealthDelay(state *model.AlertEvaluationState, observation model.AlertEvaluationObservation) {
	health := &state.Health
	if health.Status != observation.Status || health.Suspended {
		health.Status, health.ElapsedMS = observation.Status, 0
		health.StatusSince = timePointer(observation.ObservedAt)
	} else if health.LastObservedAt != nil && state.RunID == observation.RunID {
		delta := observation.ObservedAt.Sub(*health.LastObservedAt)
		if delta >= 0 && delta <= MaximumPollGap {
			health.ElapsedMS = min(health.ElapsedMS+delta.Milliseconds(), int64(MaximumDelaySeconds)*1000)
		} else {
			health.ElapsedMS = 0
		}
	}
	health.LastObservedAt = timePointer(observation.ObservedAt)
	health.Suspended = false
}

func healthPolicy(rule model.AlertEvaluationRule, status string) (int, string, int) {
	if status == model.EvaluationNoData {
		return noDataMask, rule.NoDataPolicy, rule.NoDataDelaySeconds
	}
	return errorMask, rule.ErrorPolicy, rule.ErrorDelaySeconds
}

func finishHealth(result *StepResult, observation model.AlertEvaluationObservation) {
	if result.State.Health.EpochID == "" {
		return
	}
	event := healthEvent(result.State, "recovered", "fresh_evidence", observation.ObservedAt)
	notify := result.State.Health.NotifiedMask != 0
	if notify {
		event.NotifiedAt = timePointer(observation.ObservedAt)
	}
	event.ClosedAt = timePointer(observation.ObservedAt)
	result.Actions = append(result.Actions, Action{Kind: ActionHealth, Notify: notify, Health: &event})
	result.State.Health = model.AlertEvaluationHealth{}
}

func healthEvent(state model.AlertEvaluationState, kind, reason string, at time.Time) model.AlertEvaluationEvent {
	key := "alert-evaluation:" + state.Health.EpochID + ":" + kind
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	return model.AlertEvaluationEvent{ID: id.String(), RuleID: state.RuleID, NodeID: state.NodeID, DeviceID: state.DeviceID, DeviceName: state.DeviceName, RuleRevision: state.RuleRevision,
		EpochID: state.Health.EpochID, Kind: kind, Reason: reason, OccurredAt: at, SourceKey: key}
}
