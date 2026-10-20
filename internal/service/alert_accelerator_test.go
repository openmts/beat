package service

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestGPUAlertsIsolateAlternatingDeviceWindows(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	if _, err := f.core.db.DB.Exec("UPDATE alert_rules SET duration=60 WHERE id=?", f.core.rule.ID); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 6; round++ {
		a, b := 90.0, 20.0
		if round%2 == 1 {
			a, b = b, a
		}
		f.report(t, map[string]float64{"a": a, "b": b})
		f.run(t)
		for index := range f.devices {
			if f.state(t, index).ActiveEventID != "" {
				t.Fatal("alternating devices triggered a shared incident")
			}
		}
		f.core.at = f.core.at.Add(30 * time.Second)
	}
}

func TestGPUDeviceDisappearanceResolvesOneIncidentAndQueuesOnce(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	f.report(t, map[string]float64{"a": 90, "b": 95})
	f.run(t)
	first, second := f.state(t, 0), f.state(t, 1)
	if first.ActiveEventID == "" || second.ActiveEventID == "" || first.ActiveEventID == second.ActiveEventID {
		t.Fatal("devices shared an incident")
	}
	f.core.at = f.core.at.Add(30 * time.Second)
	f.report(t, map[string]float64{"b": 95})
	f.run(t)
	resolved, err := store.NewAlertEventStore(f.core.db.DB).GetEventByID(t.Context(), first.ActiveEventID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != model.AlertStatusResolved || resolved.ResolutionReason != model.AcceleratorDeviceUnavailable || resolved.Value != 90 {
		t.Fatalf("incorrect device resolution: %+v", resolved)
	}
	if f.state(t, 1).ActiveEventID != second.ActiveEventID || f.state(t, 0).ActiveEventID != "" {
		t.Fatal("device disappearance affected another GPU")
	}
	f.core.at = f.core.at.Add(30 * time.Second)
	f.run(t)
	var messages int
	if err := f.core.db.DB.QueryRow("SELECT count(*) FROM notification_messages WHERE source_id=? AND event_kind='alert.resolved'", first.ActiveEventID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 1 {
		t.Fatalf("resolution notifications=%d", messages)
	}
}

func TestGPUHostHeartbeatsDoNotAdvanceNativeEvidence(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	if _, err := f.core.db.DB.Exec("UPDATE alert_rules SET duration=60 WHERE id=?", f.core.rule.ID); err != nil {
		t.Fatal(err)
	}
	f.report(t, map[string]float64{"a": 90, "b": 95})
	f.run(t)
	before := f.state(t, 0)
	f.core.at = f.core.at.Add(30 * time.Second)
	f.heartbeat(t)
	f.run(t)
	after := f.state(t, 0)
	if after.EvidenceID != before.EvidenceID || after.TriggerElapsedMS != 0 || after.ActiveEventID != "" {
		t.Fatal("host heartbeat credited GPU evidence")
	}
	f.core.at = f.core.at.Add(90 * time.Second)
	f.heartbeat(t)
	f.run(t)
	if f.state(t, 0).Status != model.EvaluationNoData {
		t.Fatal("old native GPU sample remained fresh")
	}
}

func TestGPUWindowAndPartialNativeSample(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	if _, err := f.core.db.DB.Exec("UPDATE alert_rules SET window_seconds=60,aggregation='avg',threshold=99 WHERE id=?", f.core.rule.ID); err != nil {
		t.Fatal(err)
	}
	for _, value := range []float64{10, 20, 30} {
		f.report(t, map[string]float64{"a": value, "b": value})
		f.core.at = f.core.at.Add(30 * time.Second)
	}
	f.core.at = f.core.at.Add(-30 * time.Second)
	if _, err := f.core.evaluator.states.Admit(t.Context(), f.core.at); err != nil {
		t.Fatal(err)
	}
	input, _, err := f.core.evaluator.prepare(t.Context(), store.AlertEvaluationKey{RuleID: f.core.rule.ID, NodeID: f.core.node.ID, DeviceID: f.devices[0]})
	if err != nil || input.Observation.Value == nil || *input.Observation.Value != 20 {
		t.Fatalf("GPU window: %+v %v", input.Observation, err)
	}
	if _, err := f.core.db.DB.Exec("UPDATE alert_rules SET window_seconds=0,aggregation='last' WHERE id=?", f.core.rule.ID); err != nil {
		t.Fatal(err)
	}
	f.core.at = f.core.at.Add(30 * time.Second)
	gpu := testutil.AcceleratorReport(f.core.at)
	gpu.CadenceMS = 30000
	delete(gpu.Devices[0].Metrics, "gpu_utilization_percent")
	f.publish(t, gpu)
	input, _, err = f.core.evaluator.prepare(t.Context(), store.AlertEvaluationKey{RuleID: f.core.rule.ID, NodeID: f.core.node.ID, DeviceID: f.devices[0]})
	if err != nil || input.Observation.Status != model.EvaluationNoData || input.Observation.Value != nil {
		t.Fatal("partial native sample reused an old value", input.Observation, err)
	}
}

func TestGPUUpgradePreservesScalarEvidenceIdentity(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	f.report(t, 90, model.CollectedCustomSamples{})
	f.run(t)
	definition, err := f.evaluator.catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.metrics.ReadAlertEvidence(t.Context(), store.AlertEvidenceRequest{NodeID: f.node.ID, Metric: "cpu", PolicyRevision: definition.Revision})
	if err != nil {
		t.Fatal(err)
	}
	source := snapshot.Points[len(snapshot.Points)-1]
	identity := fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s|%d", "cpu", source.ID, 1, "last", source.At.Format(time.RFC3339Nano), source.At.Format(time.RFC3339Nano), f.node.ID, definition.Revision)
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	if f.state(t).EvidenceID != expected {
		t.Fatal("v14 changed an unchanged scalar evidence identity")
	}
}

func TestGPUAnyDeviceEmptyInventoryHasZeroTargets(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	result, err := f.core.evaluator.RunOnce(t.Context())
	if err != nil || result.Completed != 0 || result.Failed != 0 {
		t.Fatalf("empty GPU inventory fabricated a target: %+v %v", result, err)
	}
	states, err := f.core.evaluator.states.List(t.Context(), store.AlertEvaluationQuery{RuleID: f.core.rule.ID})
	if err != nil || len(states.Items) != 0 {
		t.Fatal(states, err)
	}
	rule, err := f.core.evaluator.rules.Get(t.Context(), f.core.rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.core.evaluator.Preview(t.Context(), "preview-client", model.AlertEvaluationPreviewRequest{Rule: *rule, NodeIDs: []string{f.core.node.ID}})
	if err != nil || len(preview) != 0 {
		t.Fatal("empty GPU preview returned fake values", preview, err)
	}
}
