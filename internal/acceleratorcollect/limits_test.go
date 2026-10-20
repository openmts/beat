package acceleratorcollect

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestCollectorDeviceBoundsAndFailureInventory(t *testing.T) {
	rows := strings.Builder{}
	for range 33 {
		rows.WriteString(nvidiaFixture)
	}
	if _, err := ParseNVIDIA([]byte(rows.String())); err == nil {
		t.Fatal("oversized enumeration accepted")
	}
	executor := &fixtureExecutor{outputs: map[string]string{"nvidia": nvidiaFixture, "amd": amdFixture}, errors: map[string]error{}}
	collector, err := New(Config{Enabled: true, MaximumDevices: 1}, executor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := collector.Collect(t.Context())
	if err != nil || report.State != "unavailable" || report.ErrorCode != "device_limit" || len(report.Devices) != 0 {
		t.Fatal(report, err)
	}
	for _, capability := range report.Capabilities {
		if capability.State != "unavailable" {
			t.Fatal("failure advertised complete empty inventory")
		}
	}
	collector.local.MaximumDevices = 32
	collector.local.Metrics = []string{"gpu_utilization_percent"}
	report, err = collector.Collect(t.Context())
	if err != nil || len(report.Devices[0].Metrics) != 1 {
		t.Fatal(report, err)
	}
	if err = collector.ApplyPolicy(model.AcceleratorPolicy{}); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

func TestUnavailableVendorsAndNumericAMDValues(t *testing.T) {
	executor := &fixtureExecutor{outputs: map[string]string{}, errors: map[string]error{"nvidia": os.ErrNotExist, "amd": context.DeadlineExceeded}}
	collector, err := New(Config{Enabled: true}, executor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := collector.Collect(t.Context())
	if err != nil || report.State != "unavailable" || len(report.Devices) != 0 {
		t.Fatal(report, err)
	}
	executor.errors["amd"] = os.ErrNotExist
	report, err = collector.Collect(t.Context())
	if err != nil || report.State != "unsupported" {
		t.Fatal(report, err)
	}
	raw := strings.Replace(amdFixture, `"25"`, `25`, 1)
	values, err := ParseAMD([]byte(raw))
	if err != nil || values[0].Metrics["gpu_utilization_percent"] != 25 {
		t.Fatal(values, err)
	}
	raw = strings.Replace(amdFixture, `"25"`, `{"bad":1}`, 1)
	if _, err = ParseAMD([]byte(raw)); err == nil {
		t.Fatal("malformed numeric field accepted")
	}
	if _, err = ParseAMD([]byte(`{"global":{}}`)); err == nil {
		t.Fatal("unbounded output domain")
	}
}

func TestAMDRejectsDuplicateObjectsAndNullInventory(t *testing.T) {
	for _, raw := range []string{`null`, `{"card0":{},"card0":{}}`, `{"card0":{"GPU use (%)":"1","GPU use (%)":"2"}}`} {
		if _, err := ParseAMD([]byte(raw)); err == nil {
			t.Fatal("ambiguous inventory accepted", raw)
		}
	}
}

func TestAMDSystemDriverMetadataIsBoundedAndNotADevice(t *testing.T) {
	raw := strings.Replace(amdFixture, `"Driver version":"6.0",`, "", 1)
	raw = strings.TrimSuffix(raw, "}") + `,"system":{"Driver version":"6.1"}}`
	devices, err := ParseAMD([]byte(raw))
	if err != nil || len(devices) != 1 || devices[0].DriverVersion != "6.1" {
		t.Fatal(devices, err)
	}
	if _, err = ParseAMD([]byte(`{"system":null}`)); err == nil {
		t.Fatal("invalid system metadata accepted")
	}
}
