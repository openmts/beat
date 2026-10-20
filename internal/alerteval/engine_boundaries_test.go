package alerteval

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestFreshEvidenceCanRetriggerAfterCheckedIncidentWasManuallyResolved(t *testing.T) {
	start := time.Unix(1800000000, 0)
	input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node"}, Observation: stateObservation(start, "first", 90)}
	input.Rule.Duration = 0
	first := advanceState(t, input)
	resolved := *first.Actions[0].Event
	resolved.Status = model.AlertStatusResolved
	input.Previous = first.State
	input.IncidentChecked = true
	input.Incident = &resolved
	input.Observation = stateObservation(start.Add(30*time.Second), "second", 90)
	next := advanceState(t, input)
	if len(next.Actions) != 1 || next.Actions[0].Kind != ActionThresholdTrigger || next.State.ActiveEventID == resolved.ID {
		t.Fatal("resolved history prevented a new incident")
	}
}

func TestSuspensionRetainsIncidentAndNeverFabricatesRecovery(t *testing.T) {
	start := time.Unix(1800000000, 0)
	for _, status := range []string{model.EvaluationDisabled, model.EvaluationOutOfScope} {
		input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node"}, Observation: stateObservation(start, "first", 90)}
		input.Rule.Duration = 0
		input.Previous = advanceState(t, input).State
		incident := input.Previous.ActiveEventID
		input.Observation = model.AlertEvaluationObservation{ObservedAt: start.Add(time.Second), RunID: "run", Status: status, Reason: "scope_changed"}
		result := advanceState(t, input)
		if result.State.ActiveEventID != incident || !result.State.Health.Suspended || len(result.Actions) != 0 {
			t.Fatal("suspension fabricated recovery or lost incident")
		}
	}
}

func TestClockRollbackAndLongLiveGapsCannotCompletePendingConfirmation(t *testing.T) {
	start := time.Unix(1800000000, 0)
	for _, elapsed := range []time.Duration{15 * time.Second, 2 * time.Minute} {
		input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node"}, Observation: stateObservation(start, "first", 90)}
		input.Previous = advanceState(t, input).State
		input.Observation = stateObservation(start.Add(30*time.Second), "second", 90)
		input.Previous = advanceState(t, input).State
		input.Observation = stateObservation(start.Add(elapsed), "third", 90)
		result := advanceState(t, input)
		if result.State.TriggerElapsedMS != 0 || result.State.ActiveEventID != "" {
			t.Fatal("unproven time completed confirmation")
		}
		if elapsed < 30*time.Second && result.State.Reason != "clock_rollback" {
			t.Fatal("clock rollback was hidden")
		}
	}
}

func TestObservationValidationRejectsMissingOrUnusableEvidence(t *testing.T) {
	start := time.Unix(1800000000, 0)
	for _, mutate := range []func(*StepInput){
		func(i *StepInput) { i.Rule.ID = "" }, func(i *StepInput) { i.Node.ID = "" }, func(i *StepInput) { i.Rule.Revision = 0 },
		func(i *StepInput) { i.Observation.ObservedAt = time.Time{} }, func(i *StepInput) { i.Observation.RunID = "" },
		func(i *StepInput) { i.Observation.Status = "invalid" }, func(i *StepInput) { i.Observation.Value = nil },
		func(i *StepInput) { value := math.NaN(); i.Observation.Value = &value }, func(i *StepInput) { i.Observation.EvidenceID = "" },
		func(i *StepInput) { i.Observation.SourceAt = time.Time{} }, func(i *StepInput) { i.Observation.Cadence = 0 },
		func(i *StepInput) { i.Observation.SourceAt = start.Add(model.MaximumAgentClockSkew + time.Second) },
		func(i *StepInput) { i.Observation.Status = model.EvaluationError },
	} {
		input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node"}, Observation: stateObservation(start, "first", 90)}
		mutate(&input)
		if _, err := Step(input); !errors.Is(err, ErrInvalidObservation) {
			t.Fatal("invalid observation accepted")
		}
	}
}
