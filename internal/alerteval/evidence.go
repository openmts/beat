package alerteval

import (
	"time"

	"github.com/beat/backend/internal/model"
)

func newEvidence(previous model.AlertEvaluationState, observation model.AlertEvaluationObservation) bool {
	return observation.EvidenceID != previous.EvidenceID && (previous.LastEvidenceAt == nil || !observation.SourceAt.Before(*previous.LastEvidenceAt))
}

func evidenceElapsed(previous model.AlertEvaluationState, observation model.AlertEvaluationObservation) (time.Duration, bool) {
	if previous.LastEvidenceAt == nil || previous.LastEvidenceObservedAt == nil {
		return 0, true
	}
	if previous.RunID != observation.RunID {
		return 0, true
	}
	if previous.LastEvaluatedAt != nil && observation.ObservedAt.Sub(*previous.LastEvaluatedAt) > MaximumPollGap {
		return 0, false
	}
	sourceElapsed := observation.SourceAt.Sub(*previous.LastEvidenceAt)
	maximumGap := max(2*observation.Cadence, 30*time.Second)
	if sourceElapsed < 0 || sourceElapsed > maximumGap {
		return 0, false
	}
	wallElapsed := observation.ObservedAt.Sub(*previous.LastEvidenceObservedAt)
	return max(min(sourceElapsed, wallElapsed), 0), true
}

func applyEvidence(state *model.AlertEvaluationState, observation model.AlertEvaluationObservation) {
	state.Status, state.Reason = model.EvaluationOK, ""
	state.EvidenceID, state.RunID = observation.EvidenceID, observation.RunID
	state.LastEvidenceAt = timePointer(observation.SourceAt)
	state.LastEvidenceObservedAt = timePointer(observation.ObservedAt)
	state.LastGoodAt = timePointer(observation.ObservedAt)
}

func resetConfirmation(state *model.AlertEvaluationState) {
	state.TriggerElapsedMS, state.RecoveryElapsedMS = 0, 0
	state.PendingSince, state.RecoverySince = nil, nil
	if state.ActiveEventID == "" {
		state.Phase = model.AlertPhaseNormal
	} else {
		state.Phase = model.AlertPhaseFiring
	}
}
