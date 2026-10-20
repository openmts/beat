package alerteval

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func stateRule() model.AlertEvaluationRule {
	rule := DefaultRule()
	rule.ID = "rule"
	rule.Name = "CPU"
	rule.Metric = "cpu"
	rule.Threshold = 80
	rule.Duration = 60
	rule.RecoveryDuration = 30
	recovery := 75.0
	rule.RecoveryThreshold = &recovery
	return rule
}

func stateObservation(at time.Time, id string, value float64) model.AlertEvaluationObservation {
	return model.AlertEvaluationObservation{ObservedAt: at, SourceAt: at, RunID: "run", EvidenceID: id, Cadence: 30 * time.Second, Status: model.EvaluationOK, Value: &value}
}

func advanceState(t *testing.T, input StepInput) StepResult {
	t.Helper()
	result, err := Step(input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestThresholdConfirmationCountsOnlyNewEvidenceAndSeparateRecovery(t *testing.T) {
	start := time.Unix(1800000000, 0)
	input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node", Name: "node"}, Observation: stateObservation(start, "one", 90)}
	first := advanceState(t, input)
	if first.State.Phase != model.AlertPhasePending || first.State.TriggerElapsedMS != 0 || len(first.Actions) != 0 {
		t.Fatal("initial confirmation was skipped")
	}
	input.Previous = first.State
	input.Observation.ObservedAt = start.Add(30 * time.Second)
	duplicate := advanceState(t, input)
	if duplicate.State.TriggerElapsedMS != 0 || len(duplicate.Actions) != 0 {
		t.Fatal("same receipt counted as new evidence")
	}
	input.Previous = duplicate.State
	input.Observation = stateObservation(start.Add(time.Minute), "two", 90)
	firing := advanceState(t, input)
	if firing.State.Phase != model.AlertPhaseFiring || len(firing.Actions) != 1 || firing.Actions[0].Kind != ActionThresholdTrigger {
		t.Fatal("confirmed condition did not trigger")
	}
	input.Previous = firing.State
	input.Observation = stateObservation(start.Add(90*time.Second), "three", 70)
	recovering := advanceState(t, input)
	if recovering.State.Phase != model.AlertPhaseRecovering || len(recovering.Actions) != 0 {
		t.Fatal("recovery bypassed its delay")
	}
	input.Previous = recovering.State
	input.Observation = stateObservation(start.Add(120*time.Second), "four", 70)
	recovered := advanceState(t, input)
	if recovered.State.Phase != model.AlertPhaseNormal || len(recovered.Actions) != 1 || recovered.Actions[0].Kind != ActionThresholdRecovery {
		t.Fatal("confirmed recovery did not transition")
	}
}

func TestRestartDoesNotCreditDowntimeAndAcknowledgedIncidentRemainsManual(t *testing.T) {
	start := time.Unix(1800000000, 0)
	input := StepInput{Rule: stateRule(), Node: model.Node{ID: "node"}, Observation: stateObservation(start, "one", 90)}
	input.Previous = advanceState(t, input).State
	input.Observation = stateObservation(start.Add(30*time.Second), "two", 90)
	input.Previous = advanceState(t, input).State
	input.Observation = stateObservation(start.Add(time.Hour), "three", 90)
	input.Observation.RunID = "restarted"
	restarted := advanceState(t, input)
	if restarted.State.TriggerElapsedMS != 30000 || len(restarted.Actions) != 0 {
		t.Fatal("downtime credited debounce")
	}
	input.Previous = restarted.State
	input.Observation = stateObservation(start.Add(time.Hour+30*time.Second), "four", 90)
	input.Observation.RunID = "restarted"
	firing := advanceState(t, input)
	firing.State.ActiveEventStatus = model.AlertStatusAcknowledged
	input.Previous = firing.State
	input.Rule.RecoveryDuration = 0
	input.Observation = stateObservation(start.Add(time.Hour+60*time.Second), "five", 70)
	recovered := advanceState(t, input)
	if recovered.State.ActiveEventID == "" || recovered.State.ActiveEventStatus != model.AlertStatusAcknowledged || len(recovered.Actions) != 0 {
		t.Fatal("acknowledged incident auto-resolved")
	}
}

func TestEvaluationHealthEpochIsIndependentAndRecoversWithoutClearingThreshold(t *testing.T) {
	start := time.Unix(1800000000, 0)
	rule := stateRule()
	rule.Duration = 0
	rule.NoDataPolicy = model.EvaluationNotify
	rule.ErrorPolicy = model.EvaluationNotify
	rule.NoDataDelaySeconds = 30
	rule.ErrorDelaySeconds = 30
	input := StepInput{Rule: rule, Node: model.Node{ID: "node"}, Observation: stateObservation(start, "first", 90)}
	input.Previous = advanceState(t, input).State
	incident := input.Previous.ActiveEventID
	input.Observation = model.AlertEvaluationObservation{ObservedAt: start.Add(30 * time.Second), RunID: "run", Status: model.EvaluationNoData, Reason: "empty_window"}
	input.Previous = advanceState(t, input).State
	epoch := input.Previous.Health.EpochID
	input.Observation.ObservedAt = start.Add(60 * time.Second)
	nodata := advanceState(t, input)
	if len(nodata.Actions) != 1 || !nodata.Actions[0].Notify || nodata.Actions[0].Health.Kind != "no_data" || nodata.State.ActiveEventID != incident {
		t.Fatal("NoData notice overwrote threshold")
	}
	input.Previous = nodata.State
	input.Observation.Status = model.EvaluationError
	input.Observation.Reason = "storage_unavailable"
	input.Observation.ObservedAt = start.Add(90 * time.Second)
	input.Previous = advanceState(t, input).State
	input.Observation.ObservedAt = start.Add(120 * time.Second)
	failure := advanceState(t, input)
	if failure.State.Health.EpochID != epoch || len(failure.Actions) != 1 || !failure.Actions[0].Notify {
		t.Fatal("outage kind changed epoch or skipped independent delay")
	}
	input.Previous = failure.State
	input.Observation = stateObservation(start.Add(150*time.Second), "fresh", 90)
	healthy := advanceState(t, input)
	if healthy.State.ActiveEventID != incident || len(healthy.Actions) != 1 || healthy.Actions[0].Kind != ActionHealth || healthy.Actions[0].Health.Kind != "recovered" || !healthy.Actions[0].Notify {
		t.Fatal("evaluation recovery falsely cleared threshold")
	}
}
