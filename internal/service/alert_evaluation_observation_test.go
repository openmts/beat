package service

import (
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestEvaluationObservationReportsGapsAndInvalidNumericEvidence(t *testing.T) {
	at := time.Now().UTC()
	rule := alerteval.DefaultRule()
	rule.Metric = "cpu"
	base := model.AlertEvaluationObservation{ObservedAt: at}
	receipt := &model.AgentReceipt{CapturedAt: at}
	cases := []struct {
		name     string
		snapshot store.AlertEvidenceSnapshot
		window   int
		reason   string
	}{
		{"missing receipt", store.AlertEvidenceSnapshot{}, 0, "missing_receipt"},
		{"stale receipt", store.AlertEvidenceSnapshot{Receipt: &model.AgentReceipt{CapturedAt: at.Add(-time.Hour)}, Cadence: time.Second}, 0, "stale_receipt"},
		{"missing value", store.AlertEvidenceSnapshot{Receipt: receipt, Cadence: time.Second}, 0, "missing_value"},
		{"missing end", store.AlertEvidenceSnapshot{Receipt: receipt, Cadence: time.Second, Points: []alerteval.EvidencePoint{{ID: "point", At: at.Add(-time.Second), Value: 1}}}, 10, "incomplete_window"},
		{"incomplete window", store.AlertEvidenceSnapshot{Receipt: receipt, Cadence: time.Second, Points: []alerteval.EvidencePoint{{ID: "point", At: at, Value: 1}}}, 10, "incomplete_window"},
		{"nan", store.AlertEvidenceSnapshot{Receipt: receipt, Cadence: time.Second, Points: []alerteval.EvidencePoint{{ID: "point", At: at, Value: math.NaN()}}}, 0, "invalid_evidence"},
		{"conflicting source", store.AlertEvidenceSnapshot{Receipt: receipt, Cadence: time.Second, Points: []alerteval.EvidencePoint{{ID: "same", At: at, Value: 1}, {ID: "same", At: at, Value: 2}}}, 0, "invalid_evidence"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rule.WindowSeconds = test.window
			observation := evaluationObservation(base, rule, test.snapshot)
			if observation.Reason != test.reason || observation.Value != nil {
				t.Fatal(observation)
			}
		})
	}
}

func TestEvaluationScopeMembershipKinds(t *testing.T) {
	node := model.Node{ID: "node", GroupID: "group"}
	for _, scope := range []model.AlertRuleScope{{Kind: "all"}, {Kind: "nodes", NodeIDs: []string{"node"}}, {Kind: "groups", GroupIDs: []string{"group"}}} {
		if !evaluationNodeInScope(model.AlertEvaluationRule{Scope: scope}, node) {
			t.Fatal(scope)
		}
	}
	if evaluationNodeInScope(model.AlertEvaluationRule{Scope: model.AlertRuleScope{Kind: "invalid"}}, node) {
		t.Fatal("invalid scope accepted")
	}
}
