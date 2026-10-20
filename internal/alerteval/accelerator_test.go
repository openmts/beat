package alerteval

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAcceleratorAlternatingDevicesDoNotShareDuration(t *testing.T) {
	rule := DefaultRule()
	rule.ID = "gpu-rule"
	rule.Name = "GPU utilization"
	rule.Metric = "gpu_utilization_percent"
	rule.Threshold = 80
	rule.Duration = 60
	rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{"node"}, DeviceID: model.AcceleratorAnyDevice}
	at := time.Now().UTC()
	states := map[string]model.AlertEvaluationState{}
	devices := []string{strings.Repeat("a", 32), strings.Repeat("b", 32)}
	for round := 0; round < 6; round++ {
		for index, device := range devices {
			value := 20.0
			if round%2 == index {
				value = 90
			}
			observed := at.Add(time.Duration(round) * 30 * time.Second)
			result, err := Step(StepInput{Rule: rule, Node: model.Node{ID: "node", Name: "GPU host"}, DeviceID: device, DeviceName: "GPU device", Previous: states[device], Observation: model.AlertEvaluationObservation{ObservedAt: observed, SourceAt: observed, RunID: "run", EvidenceID: observed.String() + device, Cadence: 30 * time.Second, Status: model.EvaluationOK, Value: &value}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Actions) != 0 || result.State.ActiveEventID != "" {
				t.Fatal("alternating devices incorrectly completed shared duration")
			}
			if result.State.DeviceID != device {
				t.Fatal("state lost device identity")
			}
			states[device] = result.State
		}
	}
}

func TestAcceleratorDisappearanceResolvesOnlyMatchingIncident(t *testing.T) {
	device := strings.Repeat("a", 32)
	at := time.Now().UTC()
	rule := DefaultRule()
	rule.ID = "rule"
	rule.Name = "GPU"
	rule.Metric = "gpu_utilization_percent"
	rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{"node"}, DeviceID: device}
	for _, status := range []model.AlertStatus{model.AlertStatusTriggered, model.AlertStatusAcknowledged} {
		incident := &model.AlertEvent{ID: "incident", RuleID: rule.ID, NodeID: "node", DeviceID: device, DeviceName: "GPU A", Value: 93, Status: status}
		previous := model.AlertEvaluationState{RuleID: rule.ID, NodeID: "node", DeviceID: device, ActiveEventID: incident.ID, ActiveEventStatus: status, Phase: model.AlertPhaseFiring}
		result, err := Step(StepInput{Rule: rule, Node: model.Node{ID: "node"}, DeviceID: device, DeviceName: "GPU A", Previous: previous, Incident: incident, IncidentChecked: true, Observation: model.AlertEvaluationObservation{ObservedAt: at, RunID: "run", Status: model.EvaluationOutOfScope, Reason: model.AcceleratorDeviceUnavailable}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Actions) != 1 || result.Actions[0].Kind != ActionDeviceUnavailable || result.Actions[0].PreviousStatus != status {
			t.Fatal(result)
		}
		if result.Actions[0].Event.Value != 93 || result.Actions[0].Event.ResolutionReason != model.AcceleratorDeviceUnavailable || result.State.ActiveEventID != "" {
			t.Fatal("disappearance lost original evidence or resolution")
		}
	}
}
