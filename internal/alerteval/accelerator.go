package alerteval

import "github.com/beat/backend/internal/model"

const ActionDeviceUnavailable = "device_unavailable"

func unavailableDevice(input StepInput, result StepResult) (StepResult, error) {
	state := &result.State
	state.Status, state.Reason = model.EvaluationOutOfScope, model.AcceleratorDeviceUnavailable
	state.Health.Suspended = true
	if state.ActiveEventID != "" {
		if input.Incident == nil || input.Incident.ID != state.ActiveEventID || input.Incident.DeviceID != input.DeviceID {
			return StepResult{}, ErrInvalidObservation
		}
		event := *input.Incident
		event.Status, event.ResolvedAt, event.ResolutionReason = model.AlertStatusResolved, timePointer(input.Observation.ObservedAt), model.AcceleratorDeviceUnavailable
		event.RuleName, event.NodeName, event.Metric, event.Severity = input.Rule.Name, input.Node.Name, input.Rule.Metric, input.Rule.Severity
		event.Message = input.Rule.Name + ": GPU device unavailable · " + input.DeviceName
		result.Actions = append(result.Actions, Action{Kind: ActionDeviceUnavailable, Notify: true, Event: &event, PreviousStatus: state.ActiveEventStatus})
		state.ActiveEventID, state.ActiveEventStatus = "", ""
	}
	resetConfirmation(state)
	return result, nil
}
