package service

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type gpuEvaluationFixture struct {
	core     *evaluationRuntimeFixture
	identity model.AgentIdentity
	devices  []string
}

func newGPUEvaluationFixture(t *testing.T) *gpuEvaluationFixture {
	t.Helper()
	core := newEvaluationRuntimeFixture(t)
	node, err := store.NewNodeStore(core.db.DB).CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "GPU host", Host: "host", Port: 22}, store.AgentCredential{Hash: bytes.Repeat([]byte{7}, 32), Prefix: "gpu", CreatedAt: core.at})
	if err != nil {
		t.Fatal(err)
	}
	core.node = node
	if err := core.metrics.AttachMetricCatalog(t.Context(), store.NewMetricCatalogStore(core.db.DB)); err != nil {
		t.Fatal(err)
	}
	if _, err := core.db.DB.Exec(`UPDATE alert_rules SET name='GPU load',metric='gpu_utilization_percent',scope_kind='nodes',device_id='*',duration=0 WHERE id=?`, core.rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := core.db.DB.Exec(`INSERT INTO alert_rule_nodes(rule_id,node_id) VALUES(?,?)`, core.rule.ID, node.ID); err != nil {
		t.Fatal(err)
	}
	core.rule.Name, core.rule.Metric = "GPU load", "gpu_utilization_percent"
	return &gpuEvaluationFixture{core: core, identity: model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(node.AgentTokenHash)}, devices: []string{model.AcceleratorDeviceID(node.ID, strings.Repeat("a", 64)), model.AcceleratorDeviceID(node.ID, strings.Repeat("b", 64))}}
}

func (f *gpuEvaluationFixture) report(t *testing.T, values map[string]float64) model.AcceleratorReport {
	t.Helper()
	report := testutil.AcceleratorReport(f.core.at)
	template := report.Devices[0]
	report.CadenceMS = 30000
	report.Devices = nil
	for _, name := range []string{"a", "b"} {
		value, exists := values[name]
		if !exists {
			continue
		}
		device := template
		device.Fingerprint = strings.Repeat(name, 64)
		device.Model = "GPU " + strings.ToUpper(name)
		device.Metrics = maps.Clone(template.Metrics)
		device.Metrics["gpu_utilization_percent"] = value
		report.Devices = append(report.Devices, device)
	}
	f.publish(t, report)
	return report
}

func (f *gpuEvaluationFixture) publish(t *testing.T, gpu model.AcceleratorReport) {
	t.Helper()
	report := model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: f.core.at, ReportIntervalMS: 1000, Name: f.core.node.Name, Host: "host", Port: 22, Metrics: &model.NodeMetrics{}, Accelerators: &gpu}
	f.submit(t, report)
}

func (f *gpuEvaluationFixture) submit(t *testing.T, report model.AgentReport) {
	t.Helper()
	attempt, err := f.core.metrics.PrepareAgentReport(t.Context(), store.AgentReportInput{Identity: f.identity, Report: report, Now: f.core.at}, func(ctx context.Context) (store.MetricReportSample, error) {
		return store.NewMetricCatalogStore(f.core.db.DB).PrepareAgentPolicy(ctx, store.AgentMetricPolicyInput{NodeID: f.core.node.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	_, err = store.NewNodeStore(f.core.db.DB).AcceptAgentContact(t.Context(), store.AgentContact{Identity: f.identity, Heartbeat: store.NodeHeartbeat{Host: "host", Port: 22}, Accelerators: report.Accelerators, ReportID: report.SampleID, ReportCapturedAt: report.CapturedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if report.Accelerators == nil {
		return
	}
	inventory := store.NewAcceleratorStore(f.core.db.DB)
	pending, err := inventory.Pending(t.Context(), f.core.node.ID)
	if err != nil || pending == nil {
		t.Fatal(err)
	}
	receipt, err := f.core.metrics.AcceleratorSampleAt(t.Context(), f.core.node.ID, report.Accelerators.CapturedAt.UnixNano())
	if err != nil || receipt == nil {
		t.Fatal(err)
	}
	if err := inventory.Publish(t.Context(), store.AcceleratorPublication{Pending: *pending, Receipt: *receipt}); err != nil {
		t.Fatal(err)
	}
}

func (f *gpuEvaluationFixture) run(t *testing.T) {
	t.Helper()
	result, err := f.core.evaluator.RunOnce(t.Context())
	if err != nil || result.Failed != 0 || result.Completed != 2 {
		t.Fatalf("GPU round: %+v %v", result, err)
	}
}

func (f *gpuEvaluationFixture) state(t *testing.T, index int) *model.AlertEvaluationState {
	t.Helper()
	state, err := f.core.evaluator.states.Get(t.Context(), store.AlertEvaluationKey{RuleID: f.core.rule.ID, NodeID: f.core.node.ID, DeviceID: f.devices[index]})
	if err != nil || state == nil {
		t.Fatal(err)
	}
	return state
}

func (f *gpuEvaluationFixture) heartbeat(t *testing.T) {
	f.submit(t, model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: f.core.at, ReportIntervalMS: 1000, Name: f.core.node.Name, Host: "host", Port: 22, Metrics: &model.NodeMetrics{CPU: 99}})
}
