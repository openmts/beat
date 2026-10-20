package service

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func preparedGPUDecision(t *testing.T, f *gpuEvaluationFixture) store.AlertEvaluationCommit {
	t.Helper()
	if _, err := f.core.evaluator.states.Admit(t.Context(), f.core.at); err != nil {
		t.Fatal(err)
	}
	input, policies, err := f.core.evaluator.prepare(t.Context(), store.AlertEvaluationKey{RuleID: f.core.rule.ID, NodeID: f.core.node.ID, DeviceID: f.devices[0]})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := alerteval.Step(input)
	if err != nil {
		t.Fatal(err)
	}
	return store.AlertEvaluationCommit{Decision: decision, ExpectedStateVersion: input.Previous.Version, RuleRevision: input.Rule.Revision, Policies: policies}
}

func TestGPUCommitRejectsReplacedSampleAndCrossDeviceDecision(t *testing.T) {
	for _, kind := range []string{"sample", "device"} {
		t.Run(kind, func(t *testing.T) {
			f := newGPUEvaluationFixture(t)
			f.report(t, map[string]float64{"a": 90, "b": 95})
			change := preparedGPUDecision(t, f)
			if kind == "sample" {
				f.core.at = f.core.at.Add(30 * time.Second)
				f.report(t, map[string]float64{"a": 20, "b": 20})
			} else {
				change.Decision.State.DeviceID = f.devices[1]
			}
			if _, err := f.core.evaluator.notifications.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
				t.Fatalf("stale/cross-device decision committed: %v", err)
			}
			var count int
			if err := f.core.db.DB.QueryRow("SELECT count(*) FROM alert_events").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected decision created incident", err)
			}
			if err := f.core.db.DB.QueryRow("SELECT count(*) FROM notification_messages").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected decision created notification", err)
			}
		})
	}
}

func TestGPUStatePaginationAndRemovedDimensionRemainDistinct(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	f.report(t, map[string]float64{"a": 90, "b": 95})
	f.run(t)
	first, err := f.core.evaluator.states.List(t.Context(), store.AlertEvaluationQuery{RuleID: f.core.rule.ID, Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := f.core.evaluator.states.List(t.Context(), store.AlertEvaluationQuery{RuleID: f.core.rule.ID, Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || first.Items[0].DeviceID == second.Items[0].DeviceID {
		t.Fatal(second, err)
	}
	if first.Items[0].DeviceName == "" || second.Items[0].DeviceName == "" {
		t.Fatal("state rows lost readable device names")
	}
	if _, err := f.core.db.DB.Exec("UPDATE alert_rules SET device_id=?,revision=revision+1 WHERE id=?", f.devices[1], f.core.rule.ID); err != nil {
		t.Fatal(err)
	}
	f.core.at = f.core.at.Add(30 * time.Second)
	f.run(t)
	if f.state(t, 0).Status != model.EvaluationOutOfScope {
		t.Fatal("removed device binding was retargeted")
	}
}
