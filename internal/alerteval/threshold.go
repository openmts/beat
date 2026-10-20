package alerteval

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func advanceThreshold(input StepInput, state model.AlertEvaluationState, observation model.AlertEvaluationObservation, elapsed time.Duration, result *StepResult) error {
	if state.ActiveEventID != "" {
		advanceRecovery(input.Rule, observation, elapsed, result)
		for _, action := range result.Actions {
			if action.Event != nil && input.DeviceID != "" {
				action.Event.DeviceName = input.DeviceName
				action.Event.Message += " · " + input.DeviceName
			}
		}
		return nil
	}
	if !Triggered(*observation.Value, input.Rule) {
		resetConfirmation(&result.State)
		return nil
	}
	if state.Phase != model.AlertPhasePending {
		result.State.Phase = model.AlertPhasePending
		result.State.PendingSince = timePointer(observation.ObservedAt)
		result.State.TriggerElapsedMS = 0
	} else {
		result.State.TriggerElapsedMS = min(state.TriggerElapsedMS+elapsed.Milliseconds(), int64(MaximumDelaySeconds)*1000)
	}
	if result.State.TriggerElapsedMS < int64(input.Rule.Duration)*1000 {
		return nil
	}
	return triggerThreshold(input, result, observation)
}

func triggerThreshold(input StepInput, result *StepResult, observation model.AlertEvaluationObservation) error {
	id, err := uuid.NewRandom()
	if err != nil {
		return ErrUnavailable
	}
	event := &model.AlertEvent{ID: id.String(), RuleID: input.Rule.ID, NodeID: input.Node.ID, DeviceID: input.DeviceID, DeviceName: input.DeviceName, Status: model.AlertStatusTriggered,
		TriggeredAt: observation.ObservedAt, Value: *observation.Value, RuleName: input.Rule.Name, NodeName: input.Node.Name, Metric: input.Rule.Metric, Severity: input.Rule.Severity,
		Message: fmt.Sprintf("%s: %s on %s exceeds its alert condition", input.Rule.Name, input.Rule.Metric, input.Node.Name)}
	if input.DeviceID != "" {
		event.Message += " · " + input.DeviceName
	}
	result.State.ActiveEventID, result.State.ActiveEventStatus = event.ID, event.Status
	result.State.Phase = model.AlertPhaseFiring
	result.Actions = append(result.Actions, Action{Kind: ActionThresholdTrigger, Notify: true, Event: event})
	return nil
}

func advanceRecovery(rule model.AlertEvaluationRule, observation model.AlertEvaluationObservation, elapsed time.Duration, result *StepResult) {
	state := &result.State
	if !Recovered(*observation.Value, rule) {
		state.Phase = model.AlertPhaseFiring
		state.RecoveryElapsedMS = 0
		state.RecoverySince = nil
		return
	}
	if state.Phase != model.AlertPhaseRecovering {
		state.Phase = model.AlertPhaseRecovering
		state.RecoverySince = timePointer(observation.ObservedAt)
		state.RecoveryElapsedMS = 0
	} else {
		state.RecoveryElapsedMS = min(state.RecoveryElapsedMS+elapsed.Milliseconds(), int64(MaximumDelaySeconds)*1000)
	}
	if state.RecoveryElapsedMS < int64(rule.RecoveryDuration)*1000 || state.ActiveEventStatus == model.AlertStatusAcknowledged {
		return
	}
	event := &model.AlertEvent{ID: state.ActiveEventID, RuleID: state.RuleID, NodeID: state.NodeID, DeviceID: state.DeviceID, Status: model.AlertStatusResolved, ResolvedAt: timePointer(observation.ObservedAt),
		Value: *observation.Value, RuleName: rule.Name, Metric: rule.Metric, Severity: rule.Severity, Message: rule.Name + ": alert condition recovered"}
	result.Actions = append(result.Actions, Action{Kind: ActionThresholdRecovery, Notify: true, Event: event})
	state.ActiveEventID, state.ActiveEventStatus = "", ""
	resetConfirmation(state)
}
