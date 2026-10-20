package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

func (f *acceleratorFixture) commitReport(t *testing.T, report model.AgentReport) {
	t.Helper()
	attempt := f.prepare(t, report)
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if err := f.publish(t); err != nil {
		t.Fatal(err)
	}
}

func TestAcceleratorNativeCadenceDeviceQueriesAndDisappearance(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	f.commitReport(t, report)
	devices, err := f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := devices[0].DeviceID
	nativeAt := devices[0].LastSeenAt
	f.now = f.now.Add(time.Second)
	report.CapturedAt = f.now
	report.SampleID = uuid.NewString()
	f.commitReport(t, report)
	devices, err = f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || !devices[0].LastSeenAt.Equal(nativeAt) {
		t.Fatal("host cadence refreshed GPU inventory", devices, err)
	}
	service := metricquery.NewService(NewMetricCatalogStore(f.database.DB), f.nodes, f.metrics)
	request := metricquery.Request{From: nativeAt.Add(-time.Second).Truncate(time.Millisecond), To: nativeAt.Add(time.Second).Truncate(time.Millisecond), MaxPoints: 2, Series: []metricquery.SeriesRequest{{RefID: "gpu", NodeID: f.node.ID, DeviceID: deviceID, Metric: "gpu_utilization_percent", Aggregation: "last"}}}
	result, err := service.Query(t.Context(), request, metricquery.Access{PublicOnly: true})
	if err != nil || result.Series[0].DeviceID != deviceID || !strings.Contains(result.Series[0].Label, "Synthetic GPU") {
		t.Fatal(result, err)
	}
	found := false
	for _, point := range result.Series[0].Points {
		if point.Value != nil {
			found = *point.Value == 73.125
		}
	}
	if !found {
		t.Fatal("native GPU value missing")
	}
	f.now = f.now.Add(5 * time.Second)
	absent := f.report()
	absent.Accelerators.Devices = nil
	f.commitReport(t, absent)
	devices, err = f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || devices[0].InactiveAt == nil {
		t.Fatal(devices, err)
	}
	if _, err = service.Query(t.Context(), request, metricquery.Access{PublicOnly: true}); err == nil {
		t.Fatal("inactive device publicly queried")
	}
	if _, err = service.Query(t.Context(), request, metricquery.Access{}); err != nil {
		t.Fatal("inactive history inaccessible to admin", err)
	}
}

func TestAcceleratorQueryRejectsMissingOrMismatchedDeviceDimension(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	f.commitReport(t, report)
	service := metricquery.NewService(NewMetricCatalogStore(f.database.DB), f.nodes, f.metrics)
	selection := metricquery.SeriesRequest{RefID: "gpu", NodeID: f.node.ID, Metric: "gpu_utilization_percent"}
	request := metricquery.Request{From: f.now.Add(-time.Minute).Truncate(time.Millisecond), To: f.now.Truncate(time.Millisecond), MaxPoints: 2, Series: []metricquery.SeriesRequest{selection}}
	if _, err := service.Query(t.Context(), request, metricquery.Access{}); err == nil {
		t.Fatal("legacy scalar GPU query accepted")
	}
	request.Series[0].DeviceID = strings.Repeat("b", 32)
	if _, err := service.Query(t.Context(), request, metricquery.Access{}); err == nil {
		t.Fatal("unknown device accepted")
	}
	request.Series[0].Metric = "cpu"
	if _, err := service.Query(t.Context(), request, metricquery.Access{}); err == nil {
		t.Fatal("device dimension on host metric accepted")
	}
	if err := f.metrics.WriteMetric(t.Context(), f.node.ID, "gpu_utilization_percent", 1, f.now); err != ErrMetricIngestion {
		t.Fatal("scalar GPU writer accepted", err)
	}
}

func TestAcceleratorIdentityCannotRetimestampCachedSample(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	f.commitReport(t, report)
	f.now = f.now.Add(5 * time.Second)
	report.SampleID = uuid.NewString()
	report.CapturedAt = f.now
	report.Accelerators.CapturedAt = report.Accelerators.CapturedAt.Add(5 * time.Second)
	if _, err := f.metrics.PrepareAgentReport(t.Context(), AgentReportInput{Identity: f.identity, Report: report, Now: f.now}, nil); err != ErrAgentSampleConflict {
		t.Fatal("same GPU sample retimestamped", err)
	}
	report.Accelerators.SampleID = uuid.NewString()
	report.Accelerators.CapturedAt = report.Accelerators.CapturedAt.Add(-10 * time.Second)
	if _, err := f.metrics.PrepareAgentReport(t.Context(), AgentReportInput{Identity: f.identity, Report: report, Now: f.now}, nil); err != ErrAgentSampleOrder {
		t.Fatal("old GPU sample accepted", err)
	}
}

func TestAcceleratorFailedCollectionPreservesInventory(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	f.commitReport(t, report)
	f.now = f.now.Add(5 * time.Second)
	failed := f.report()
	failed.Accelerators.State = "unavailable"
	failed.Accelerators.ErrorCode = "collector_failed"
	failed.Accelerators.Devices = nil
	failed.Accelerators.Capabilities[0].State = "unavailable"
	failed.Accelerators.Capabilities[0].ErrorCode = "collector_failed"
	f.commitReport(t, failed)
	devices, err := f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(devices) != 1 || devices[0].InactiveAt != nil {
		t.Fatal("collector failure erased inventory", devices, err)
	}
}

func TestAcceleratorEnrollmentDefaultCopiesOnce(t *testing.T) {
	f := newAcceleratorFixture(t)
	template := AcceleratorPolicyChange{Scope: AcceleratorPolicyScope{Kind: "enrollment_policy", ID: "default"}, Write: model.AcceleratorPolicyWrite{Enabled: true, SampleIntervalSeconds: 10, MaximumDevices: 2, Metrics: []string{"gpu_utilization_percent"}}}
	if _, err := f.inventory.SavePolicy(t.Context(), f.actor, template); err != nil {
		t.Fatal(err)
	}
	if err := applyAcceleratorEnrollmentPolicy(t.Context(), f.database.DB, f.node.ID); err != nil {
		t.Fatal(err)
	}
	nodePolicy, err := f.inventory.AgentPolicy(t.Context(), f.node.ID, false)
	if err != nil || !nodePolicy.Enabled || !nodePolicy.Configured || nodePolicy.SampleIntervalSeconds != 10 {
		t.Fatal(nodePolicy, err)
	}
	template.Write.ExpectedRevision = 1
	template.Write.Enabled = false
	if _, err = f.inventory.SavePolicy(t.Context(), f.actor, template); err != nil {
		t.Fatal(err)
	}
	if err = applyAcceleratorEnrollmentPolicy(t.Context(), f.database.DB, f.node.ID); err != nil {
		t.Fatal(err)
	}
	nodePolicy, err = f.inventory.AgentPolicy(t.Context(), f.node.ID, false)
	if err != nil || !nodePolicy.Enabled || nodePolicy.Revision != 1 {
		t.Fatal("existing node policy overwritten", nodePolicy, err)
	}
}
