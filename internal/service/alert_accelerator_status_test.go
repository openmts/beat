package service

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestGPUCollectionStatesAreNotFabricatedValues(t *testing.T) {
	gpu := testutil.AcceleratorReport(time.Now().UTC())
	snapshot := gpu.Inventory()
	policy := model.DefaultAcceleratorPolicy()
	policy.Enabled = true
	input := &alerteval.StepInput{Node: model.Node{ID: "node"}, DeviceID: model.AcceleratorDeviceID("node", gpu.Devices[0].Fingerprint), Rule: model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: "gpu_utilization_percent"}}}
	baseline := store.AlertAcceleratorContext{Found: true, Vendor: "nvidia", Policy: policy, Snapshot: &snapshot}
	for _, test := range []struct {
		name   string
		modify func(*store.AlertAcceleratorContext)
		status string
	}{
		{"missing", func(c *store.AlertAcceleratorContext) { c.Found = false }, model.EvaluationNoData},
		{"disabled", func(c *store.AlertAcceleratorContext) { c.Policy.Enabled = false }, model.EvaluationDisabled},
		{"metric disabled", func(c *store.AlertAcceleratorContext) { c.Policy.Metrics = nil }, model.EvaluationDisabled},
		{"no receipt", func(c *store.AlertAcceleratorContext) { c.Snapshot = nil }, model.EvaluationNoData},
		{"policy changed", func(c *store.AlertAcceleratorContext) { c.Policy.Revision++ }, model.EvaluationNoData},
		{"unsupported", func(c *store.AlertAcceleratorContext) { c.Snapshot.State = "unsupported" }, model.EvaluationNoData},
		{"unavailable", func(c *store.AlertAcceleratorContext) { c.Snapshot.State = "unavailable" }, model.EvaluationError},
		{"reported disabled", func(c *store.AlertAcceleratorContext) { c.Snapshot.State = "disabled" }, model.EvaluationDisabled},
		{"vendor unavailable", func(c *store.AlertAcceleratorContext) {
			c.Snapshot.State = "partial"
			c.Snapshot.Capabilities = []model.AcceleratorCapability{{Vendor: "nvidia", State: "unavailable"}}
		}, model.EvaluationError},
		{"other vendor", func(c *store.AlertAcceleratorContext) {
			c.Snapshot.Capabilities = []model.AcceleratorCapability{{Vendor: "amd", State: "ok"}}
		}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := baseline
			native := snapshot
			copy.Snapshot = &native
			test.modify(&copy)
			status, _ := acceleratorCollectionStatus(input, copy)
			if status != test.status {
				t.Fatalf("%s: %s", test.name, status)
			}
		})
	}
}

func TestGPUUnavailableCollectorPreservesActiveIncidents(t *testing.T) {
	f := newGPUEvaluationFixture(t)
	f.report(t, map[string]float64{"a": 90, "b": 95})
	f.run(t)
	before := f.state(t, 0).ActiveEventID
	f.core.at = f.core.at.Add(30 * time.Second)
	gpu := testutil.AcceleratorReport(f.core.at)
	gpu.CadenceMS = 30000
	gpu.State = "unavailable"
	gpu.Devices = nil
	gpu.Capabilities = []model.AcceleratorCapability{{Vendor: "nvidia", State: "unavailable"}}
	f.publish(t, gpu)
	f.run(t)
	state := f.state(t, 0)
	if state.Status != model.EvaluationError || state.ActiveEventID != before {
		t.Fatal("collector error was treated as device disappearance")
	}
}
