package alerteval

import (
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	ActionThresholdTrigger  = "threshold_trigger"
	ActionThresholdRecovery = "threshold_recovery"
	ActionHealth            = "evaluation_health"
)

type StepInput struct {
	DeviceID, DeviceName string
	Rule                 model.AlertEvaluationRule
	Node                 model.Node
	Previous             model.AlertEvaluationState
	Observation          model.AlertEvaluationObservation
	Incident             *model.AlertEvent
	IncidentChecked      bool
}

type Action struct {
	PreviousStatus model.AlertStatus
	Kind           string
	Notify         bool
	Event          *model.AlertEvent
	Health         *model.AlertEvaluationEvent
}

type StepResult struct {
	State   model.AlertEvaluationState
	Actions []Action
}

func Step(input StepInput) (StepResult, error) {
	if err := validateObservation(input); err != nil {
		return StepResult{}, err
	}
	result := initialStep(input)
	observation := stepObservation(input, &result)
	if observation.Status == model.EvaluationOutOfScope && observation.Reason == model.AcceleratorDeviceUnavailable && input.DeviceID != "" {
		return unavailableDevice(input, result)
	}
	if observation.Status == model.EvaluationDisabled || observation.Status == model.EvaluationOutOfScope {
		result.State.Status, result.State.Reason = observation.Status, observation.Reason
		resetConfirmation(&result.State)
		result.State.Health.Suspended = true
		return result, nil
	}
	if observation.Status != model.EvaluationOK {
		return unhealthyStep(input.Rule, result, observation)
	}
	if !newEvidence(input.Previous, observation) {
		if result.State.Health.EpochID != "" {
			observation.Status = model.EvaluationNoData
			observation.Reason = "no_new_evidence"
			observation.Value = nil
			return unhealthyStep(input.Rule, result, observation)
		}
		result.State.Reason = "no_new_evidence"
		return result, nil
	}
	elapsed, continuous := evidenceElapsed(input.Previous, observation)
	if !continuous {
		resetConfirmation(&result.State)
	}
	finishHealth(&result, observation)
	applyEvidence(&result.State, observation)
	if err := advanceThreshold(input, result.State, observation, elapsed, &result); err != nil {
		return StepResult{}, err
	}
	return result, nil
}

func initialStep(input StepInput) StepResult {
	state := input.Previous
	if state.Phase == "" {
		state.Phase = model.AlertPhaseNormal
	}
	if state.RuleRevision != 0 && state.RuleRevision != input.Rule.Revision {
		resetConfirmation(&state)
	}
	state.RuleID, state.NodeID, state.RuleRevision = input.Rule.ID, input.Node.ID, input.Rule.Revision
	state.DeviceID, state.DeviceName = input.DeviceID, input.DeviceName
	state.Version++
	state.Status, state.Reason = input.Observation.Status, input.Observation.Reason
	state.LastEvaluatedAt = timePointer(input.Observation.ObservedAt)
	state.UpdatedAt = input.Observation.ObservedAt
	state.NextDueAt = input.Observation.ObservedAt.Add(EvaluationInterval)
	if input.IncidentChecked {
		state.ActiveEventID, state.ActiveEventStatus = "", ""
		if input.Incident != nil && (input.Incident.Status == model.AlertStatusTriggered || input.Incident.Status == model.AlertStatusAcknowledged) {
			state.ActiveEventID, state.ActiveEventStatus = input.Incident.ID, input.Incident.Status
		}
	}
	return StepResult{State: state, Actions: []Action{}}
}

func validateObservation(input StepInput) error {
	if input.Observation.Status != model.EvaluationDisabled && input.Observation.Status != model.EvaluationOutOfScope {
		if model.IsAcceleratorAlertMetric(input.Rule.Metric) {
			if !model.ValidAcceleratorDeviceID(input.DeviceID) || (input.Rule.Scope.DeviceID != model.AcceleratorAnyDevice && input.Rule.Scope.DeviceID != input.DeviceID) {
				return ErrInvalidObservation
			}
		} else if input.DeviceID != "" {
			return ErrInvalidObservation
		}
	}
	if input.Observation.Reason == model.AcceleratorDeviceUnavailable && (!model.IsAcceleratorAlertMetric(input.Rule.Metric) || !model.ValidAcceleratorDeviceID(input.DeviceID)) {
		return ErrInvalidObservation
	}
	observation := input.Observation
	if input.Rule.ID == "" || input.Node.ID == "" || input.Rule.Revision < 1 || observation.ObservedAt.IsZero() || observation.RunID == "" {
		return ErrInvalidObservation
	}
	if !slices.Contains([]string{model.EvaluationOK, model.EvaluationNoData, model.EvaluationError, model.EvaluationDisabled, model.EvaluationOutOfScope}, observation.Status) {
		return ErrInvalidObservation
	}
	if observation.Status == model.EvaluationOK {
		if observation.Value == nil || !finite(*observation.Value) || observation.EvidenceID == "" || len(observation.EvidenceID) > 256 || observation.SourceAt.IsZero() {
			return ErrInvalidObservation
		}
		if observation.SourceAt.After(observation.ObservedAt.Add(model.MaximumAgentClockSkew)) || observation.Cadence <= 0 || observation.Cadence > model.MaximumAgentReportInterval {
			return ErrInvalidObservation
		}
	} else if observation.Value != nil {
		return ErrInvalidObservation
	}
	return nil
}

func timePointer(value time.Time) *time.Time { value = value.UTC(); return &value }

func stepObservation(input StepInput, result *StepResult) model.AlertEvaluationObservation {
	observation := input.Observation
	if !input.Rule.Enabled || input.Rule.DeletedAt != nil {
		observation.Status = model.EvaluationDisabled
		observation.Reason = "rule_disabled"
		observation.Value = nil
	}
	if input.Previous.LastEvaluatedAt != nil && observation.ObservedAt.Before(*input.Previous.LastEvaluatedAt) {
		observation.Status = model.EvaluationError
		observation.Reason = "clock_rollback"
		observation.Value = nil
		resetConfirmation(&result.State)
		result.State.Health.ElapsedMS = 0
	}
	return observation
}
