package observability

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryRecordsRuntimeCaptureEvents(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveRuntimeCaptureEvent("cpu", "started")
	registry.ObserveRuntimeCaptureEvent("cpu", "started")
	registry.ObserveRuntimeCaptureEvent("heap", "downloaded")
	registry.ObserveRuntimeCaptureEvent("unknown", "started")
	registry.ObserveRuntimeCaptureEvent("cpu", "unknown")
	registry.ObserveRuntimeCaptureRejection("rate_limited")
	registry.ObserveRuntimeCaptureRejection("not-a-reason")
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_runtime_capture_events_total{kind="cpu",result="started"} 2`,
		`beat_runtime_capture_events_total{kind="heap",result="downloaded"} 1`,
		`beat_runtime_capture_rejections_total{reason="rate_limited"} 1`,
		"# TYPE beat_runtime_captures_active gauge",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
	for _, unexpected := range []string{"kind=\"unknown\"", "reason=\"not-a-reason\""} {
		if strings.Contains(metrics, unexpected) {
			t.Fatalf("unknown enum label leaked: %s", unexpected)
		}
	}
	if len(registry.runtimeEvents) != 2 || len(registry.runtimeRejections) != 1 ||
		registry.runtimeRejections["rate_limited"] != 1 {
		t.Fatalf("events=%v rejections=%v", registry.runtimeEvents, registry.runtimeRejections)
	}
}

func TestRegistryKeepsRuntimeStatisticsOnSourceFailure(t *testing.T) {
	registry := NewRegistry()
	if stats := registry.refreshRuntimeDiagnosticsStatistics(); stats != (RuntimeDiagnosticsStatistics{}) {
		t.Fatalf("snapshot without source = %+v", stats)
	}
	healthy := RuntimeDiagnosticsStatistics{Active: 3, Bytes: 2048, CleanupFailures: 1}
	registry.SetRuntimeDiagnosticsStatistics(func() (RuntimeDiagnosticsStatistics, error) { return healthy, nil })
	if stats := registry.refreshRuntimeDiagnosticsStatistics(); stats != healthy {
		t.Fatalf("snapshot = %+v", stats)
	}
	registry.SetRuntimeDiagnosticsStatistics(func() (RuntimeDiagnosticsStatistics, error) {
		return RuntimeDiagnosticsStatistics{}, errors.New("scanner unavailable")
	})
	if stats := registry.refreshRuntimeDiagnosticsStatistics(); stats != healthy {
		t.Fatalf("failed source replaced snapshot: %+v", stats)
	}
	metrics := registry.Prometheus()
	for _, expected := range []string{
		"beat_runtime_captures_active 3",
		"beat_runtime_capture_storage_bytes 2048",
		"beat_runtime_capture_cleanup_failures_total 1",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestCloneRuntimeEventsIsolatesMutations(t *testing.T) {
	source := map[runtimeEventKey]uint64{{kind: "cpu", result: "started"}: 1}
	copied := cloneRuntimeEvents(source)
	copied[runtimeEventKey{kind: "cpu", result: "started"}] = 9
	copied[runtimeEventKey{kind: "trace", result: "failed"}] = 1
	if source[runtimeEventKey{kind: "cpu", result: "started"}] != 1 || len(source) != 1 {
		t.Fatalf("source mutated: %v", source)
	}
}
