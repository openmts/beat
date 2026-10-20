package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func acceleratorFixture() AcceleratorReport {
	return AcceleratorReport{Version: 1, SampleID: "11111111-1111-4111-8111-111111111111", CapturedAt: time.Now().UTC(), CadenceMS: 5000, CollectorVersion: "1", InstallationEnabled: true, State: "ok",
		Capabilities: []AcceleratorCapability{{Vendor: "nvidia", State: "ok", Metrics: append([]string(nil), AcceleratorMetricKeys...)}},
		Devices:      []AcceleratorDeviceReport{{Fingerprint: strings.Repeat("a", 64), Vendor: "nvidia", Model: "Fixture GPU", DriverVersion: "fixture", Capabilities: append([]string(nil), AcceleratorMetricKeys...), Metrics: map[string]float64{"gpu_utilization_percent": 42, "gpu_memory_used_bytes": 2048, "gpu_memory_total_bytes": 4096, "gpu_temperature_celsius": 50, "gpu_power_watts": 100, "gpu_power_limit_watts": 200}}}}
}

func TestAcceleratorReportRejectsInvalidProtocolIdentityAndValues(t *testing.T) {
	edits := []func(*AcceleratorReport){
		func(r *AcceleratorReport) { r.Version = 2 }, func(r *AcceleratorReport) { r.SampleID = "bad" }, func(r *AcceleratorReport) { r.CollectorVersion = "bad" }, func(r *AcceleratorReport) { r.PolicyRevision = -1 },
		func(r *AcceleratorReport) { r.State = "fake" }, func(r *AcceleratorReport) { r.ErrorCode = "raw vendor error" }, func(r *AcceleratorReport) { r.CadenceMS = 1 }, func(r *AcceleratorReport) { r.CapturedAt = r.CapturedAt.Add(time.Hour) },
		func(r *AcceleratorReport) { r.Devices = append(r.Devices, r.Devices[0]) }, func(r *AcceleratorReport) { r.Devices[0].Fingerprint = "raw PCI" }, func(r *AcceleratorReport) { r.Devices[0].Vendor = "intel" },
		func(r *AcceleratorReport) { r.Devices[0].Model = "" }, func(r *AcceleratorReport) { r.Devices[0].DriverVersion = "bad\nversion" }, func(r *AcceleratorReport) { r.Devices[0].Metrics["gpu_utilization_percent"] = 101 },
		func(r *AcceleratorReport) { r.Devices[0].Metrics["gpu_memory_used_bytes"] = 5000 }, func(r *AcceleratorReport) { r.Devices[0].Metrics["gpu_temperature_celsius"] = math.Inf(1) },
		func(r *AcceleratorReport) { r.Devices[0].Metrics["serial"] = 3 }, func(r *AcceleratorReport) { r.Capabilities = append(r.Capabilities, r.Capabilities[0]) }, func(r *AcceleratorReport) { r.State = "disabled" },
	}
	for index, edit := range edits {
		value := acceleratorFixture()
		now := value.CapturedAt
		edit(&value)
		if err := value.Validate(now); err == nil {
			t.Fatalf("invalid case %d accepted", index)
		}
	}
}

func TestAcceleratorInventoryHasNoReadingsAndPreservesProvenance(t *testing.T) {
	report := acceleratorFixture()
	if err := report.Validate(report.CapturedAt); err != nil {
		t.Fatal(err)
	}
	inventory := report.Inventory()
	if err := inventory.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"metrics":{`) || strings.Contains(string(encoded), `"gpu_utilization_percent":42`) {
		t.Fatal("inventory contains numeric readings")
	}
	decoded, err := DecodeAcceleratorInventory(string(encoded))
	if err != nil || decoded.CadenceMS != 5000 || decoded.SampleID != report.SampleID {
		t.Fatal(decoded, err)
	}
	first, err := report.Digest()
	if err != nil {
		t.Fatal(err)
	}
	report.CapturedAt = report.CapturedAt.In(time.FixedZone("offset", 3600))
	second, err := report.Digest()
	if err != nil || first != second {
		t.Fatal("UTC digest changed", err)
	}
	clone := CloneAcceleratorReport(&report)
	clone.Devices[0].Metrics["gpu_utilization_percent"] = 0
	clone.Capabilities[0].Metrics[0] = "changed"
	if report.Devices[0].Metrics["gpu_utilization_percent"] != 42 || report.Capabilities[0].Metrics[0] == "changed" {
		t.Fatal("mutable report clone")
	}
	if CloneAcceleratorReport(nil) != nil {
		t.Fatal("nil clone")
	}
	if !ValidAcceleratorDeviceID(AcceleratorDeviceID("node", report.Devices[0].Fingerprint)) || AcceleratorDeviceID("node", report.Devices[0].Fingerprint) == AcceleratorDeviceID("other", report.Devices[0].Fingerprint) {
		t.Fatal("device alias not node local")
	}
}

func TestAcceleratorStrictJSONRejectsUnknownAndDuplicateKeys(t *testing.T) {
	encoded, err := json.Marshal(acceleratorFixture())
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{`{"unknown":1}`, `{"version":1,"version":1}`, `{"devices":[{"metrics":{"gpu_power_watts":1,"gpu_power_watts":2}}]}`, `{} {}`, `[`, strings.Repeat(" ", MaximumAcceleratorSnapshotBytes+1)}
	for _, input := range cases {
		var value AcceleratorReport
		if err = json.Unmarshal([]byte(input), &value); err == nil {
			t.Fatal("invalid JSON accepted", input)
		}
	}
	var decoded AcceleratorReport
	if err = json.Unmarshal(encoded, &decoded); err != nil || decoded.Devices[0].Metrics["gpu_memory_total_bytes"] != 4096 {
		t.Fatal(err)
	}
}

func TestAcceleratorPolicyAndOptionalMetrics(t *testing.T) {
	report := acceleratorFixture()
	delete(report.Devices[0].Metrics, "gpu_power_watts")
	report.Devices[0].Errors = []AcceleratorMetricError{{Metric: "gpu_power_watts", Code: "unavailable"}}
	if err := report.Validate(report.CapturedAt); err != nil {
		t.Fatal(err)
	}
	report.Devices[0].Errors = append(report.Devices[0].Errors, report.Devices[0].Errors[0])
	if err := report.Validate(report.CapturedAt); err == nil {
		t.Fatal("duplicate error accepted")
	}
	policy := DefaultAcceleratorPolicy()
	if policy.Enabled || policy.Configured || policy.MaximumDevices != 32 {
		t.Fatal(policy)
	}
	input := AcceleratorPolicyWrite{SampleIntervalSeconds: 5, MaximumDevices: 32, Metrics: policy.Metrics}
	if err := ValidateAcceleratorPolicy(input); err != nil {
		t.Fatal(err)
	}
	input.MaximumDevices = 33
	if err := ValidateAcceleratorPolicy(input); err == nil {
		t.Fatal("device cap ignored")
	}
	for _, values := range []map[string]float64{{"gpu_power_watts": -1}, {"gpu_temperature_celsius": 251}, {"gpu_memory_used_bytes": 0.5}, {"gpu_power_limit_watts": 1000001}} {
		if err := ValidateAcceleratorValues(values, AcceleratorMetricKeys); err == nil {
			t.Fatal(values)
		}
	}
}
