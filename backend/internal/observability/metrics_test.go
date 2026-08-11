package observability

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryPrometheus(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveHTTP("GET", "GET /api/v1/nodes/{id}", 200, 20*time.Millisecond)
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_http_requests_total{method="GET",route="GET /api/v1/nodes/{id}",status="200"} 1`,
		`beat_http_request_duration_seconds_count{method="GET",route="GET /api/v1/nodes/{id}"} 1`,
		"beat_process_uptime_seconds", "beat_process_goroutines", "beat_process_memory_bytes",
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestRegistryBoundsHTTPLabels(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveHTTP("CUSTOM", strings.Repeat("x", 200), 418, time.Millisecond)
	registry.ObserveHTTP("GET", "GET /z", 500, 2*time.Millisecond)
	registry.ObserveHTTP("GET", "GET /a", 200, 3*time.Millisecond)
	registry.ObserveHTTP("POST", "", 201, 4*time.Millisecond)
	metrics := registry.Prometheus()
	if !strings.Contains(metrics, `method="OTHER",route="oversized"`) {
		t.Fatalf("unbounded labels were retained:\n%s", metrics)
	}
	if !strings.Contains(metrics, `method="POST",route="unmatched"`) {
		t.Fatalf("empty route was not normalized:\n%s", metrics)
	}
}
