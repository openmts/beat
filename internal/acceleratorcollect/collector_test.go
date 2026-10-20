package acceleratorcollect

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const nvidiaFixture = "00000000:01:00.0, Fixture NVIDIA, 555.1, 42, 2048, 4096, 55, 120.5, 250\n"

const amdFixture = `{"card0":{"PCI Bus":"0000:02:00.0","Card series":"Fixture AMD","Driver version":"6.0","GPU use (%)":"25","VRAM Total Used Memory (B)":"1024","VRAM Total Memory (B)":"4096","Temperature (Sensor edge) (C)":"50.5","Average Graphics Package Power (W)":"80","Max Graphics Package Power (W)":"200"}}`

type fixtureExecutor struct {
	outputs map[string]string
	errors  map[string]error
	calls   int
}

func (e *fixtureExecutor) Run(_ context.Context, vendor string) ([]byte, error) {
	e.calls++
	return []byte(e.outputs[vendor]), e.errors[vendor]
}

func TestBothCollectorsNormalizeUnitsAndStableIdentity(t *testing.T) {
	nvidia, err := ParseNVIDIA([]byte(nvidiaFixture))
	if err != nil || len(nvidia) != 1 {
		t.Fatal(nvidia, err)
	}
	if nvidia[0].Metrics["gpu_memory_used_bytes"] != 2048*1024*1024 || nvidia[0].Metrics["gpu_power_watts"] != 120.5 {
		t.Fatal(nvidia)
	}
	amd, err := ParseAMD([]byte(amdFixture))
	if err != nil || len(amd) != 1 || amd[0].Metrics["gpu_memory_used_bytes"] != 1024 {
		t.Fatal(amd, err)
	}
	first, err := fingerprint("nvidia", "00000000:01:00.0", " Fixture NVIDIA ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fingerprint("nvidia", "0000:01:00.0", "Fixture NVIDIA")
	if err != nil || first != second {
		t.Fatal(first, second, err)
	}
	for _, raw := range []string{nvidiaFixture + nvidiaFixture, strings.Replace(nvidiaFixture, "42,", "1,5,", 1), strings.Replace(nvidiaFixture, "2048, 4096", "5000, 4096", 1), strings.Replace(nvidiaFixture, "00000000:01:00.0", "raw-serial", 1)} {
		if _, err = ParseNVIDIA([]byte(raw)); err == nil {
			t.Fatal("bad nvidia output accepted")
		}
	}
	for _, raw := range []string{`{`, `{} {}`, strings.Replace(amdFixture, `"25"`, `"101"`, 1), strings.Replace(amdFixture, `"0000:02:00.0"`, `"unknown"`, 1)} {
		if _, err = ParseAMD([]byte(raw)); err == nil {
			t.Fatal("bad AMD output accepted")
		}
	}
}

func TestDisabledUnsupportedAndPartialNeverInventZeros(t *testing.T) {
	executor := &fixtureExecutor{outputs: map[string]string{"nvidia": nvidiaFixture}, errors: map[string]error{"amd": os.ErrNotExist}}
	collector, err := New(Config{}, executor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := collector.Collect(t.Context())
	if err != nil || report.State != "disabled" || executor.calls != 0 {
		t.Fatal(report, err)
	}
	collector.local.Enabled = true
	report, err = collector.Collect(t.Context())
	if err != nil || report.State != "partial" || len(report.Devices) != 1 {
		t.Fatal(report, err)
	}
	if _, ok := report.Devices[0].Metrics["invented"]; ok {
		t.Fatal("invented sample")
	}
	collector.platform = "windows"
	executor.calls = 0
	report, err = collector.Collect(t.Context())
	if err != nil || report.State != "unsupported" || executor.calls != 0 {
		t.Fatal(report, err)
	}
	collector.platform = "linux"
	executor.outputs["nvidia"] = strings.Replace(nvidiaFixture, "120.5,", "N/A,", 1)
	report, err = collector.Collect(t.Context())
	if err != nil || len(report.Devices[0].Errors) != 1 {
		t.Fatal(report, err)
	}
	if _, ok := report.Devices[0].Metrics["gpu_power_watts"]; ok {
		t.Fatal("missing power zero filled")
	}
}

func TestServerPolicyOverrideAndNativeSnapshotImmutability(t *testing.T) {
	executor := &fixtureExecutor{outputs: map[string]string{"nvidia": nvidiaFixture, "amd": amdFixture}, errors: map[string]error{}}
	collector, err := New(Config{Enabled: true, SampleIntervalSeconds: 5}, executor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	collector.latest = report
	first := collector.Snapshot()
	first.Devices[0].Model = "changed"
	if collector.Snapshot().Devices[0].Model == "changed" {
		t.Fatal("mutable cache")
	}
	policy := model.DefaultAcceleratorPolicy()
	policy.Configured = true
	policy.Enabled = false
	policy.Revision = 1
	if err = collector.ApplyPolicy(policy); err != nil {
		t.Fatal(err)
	}
	disabled, err := collector.Collect(t.Context())
	if err != nil || disabled.State != "disabled" || !disabled.InstallationEnabled {
		t.Fatal(disabled, err)
	}
	collector.latest = disabled
	if err = collector.ApplyPolicy(policy); err != nil || collector.Snapshot() == nil {
		t.Fatal("unchanged policy restarted collection", err)
	}
	policy.Configured = false
	policy.Revision = 0
	if err = collector.ApplyPolicy(policy); err != nil {
		t.Fatal(err)
	}
	enabled, err := collector.Collect(t.Context())
	if err != nil || enabled.State != "ok" {
		t.Fatal(enabled, err)
	}
	if enabled.CadenceMS != 5000 || !enabled.CapturedAt.Equal(enabled.Inventory().CapturedAt) {
		t.Fatal("host cadence substituted")
	}
}

func TestCollectionFailureCodesAndLimits(t *testing.T) {
	for err, expected := range map[error]string{os.ErrNotExist: "collector_missing", context.DeadlineExceeded: "collector_timeout", ErrOutputLimit: "output_limit", ErrInvalidOutput: "parse_failed", errors.New("PRIVATE STDERR"): "collector_failed"} {
		if collectorError(err) != expected {
			t.Fatal(err)
		}
	}
	if _, err := ParseNVIDIA(make([]byte, MaximumOutputBytes+1)); err != ErrOutputLimit {
		t.Fatal(err)
	}
	if _, err := ParseAMD(make([]byte, MaximumOutputBytes+1)); err != ErrOutputLimit {
		t.Fatal(err)
	}
	if _, err := New(Config{SampleIntervalSeconds: 1}, nil); err == nil {
		t.Fatal("short cadence accepted")
	}
	collector, err := New(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err = collector.Run(ctx); err != context.DeadlineExceeded {
		t.Fatal(err)
	}
	if collector.Snapshot() == nil {
		t.Fatal("disabled capability not published")
	}
}
