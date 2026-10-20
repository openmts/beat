package observability

import (
	"errors"
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

func TestRegistryServiceTokenCounters(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveServiceTokenAuth("authenticated")
	registry.ObserveServiceTokenAuth("authenticated")
	registry.ObserveServiceTokenAuth("denied")
	registry.ObserveServiceTokenDenial("scope")
	registry.ObserveServiceTokenRateLimit("token")
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_service_token_authentications_total{outcome="authenticated"} 2`,
		`beat_service_token_authentications_total{outcome="denied"} 1`,
		`beat_service_token_authentications_total{outcome="unavailable"} 0`,
		`beat_service_token_denials_total{reason="scope"} 1`,
		`beat_service_token_denials_total{reason="resource"} 0`,
		`beat_service_token_rate_limits_total{kind="token"} 1`,
		`beat_service_token_rate_limits_total{kind="source"} 0`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestRegistryDropsUnknownServiceTokenLabels(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveServiceTokenAuth("beat_service_v1_secret")
	registry.ObserveServiceTokenDenial("192.0.2.10")
	registry.ObserveServiceTokenRateLimit("node-one")
	metrics := registry.Prometheus()
	if strings.Contains(metrics, "beat_service_v1_secret") || strings.Contains(metrics, "192.0.2.10") || strings.Contains(metrics, "node-one") {
		t.Fatalf("unbounded service token labels retained:\n%s", metrics)
	}
}

func TestRegistryServiceTokenLatencyAndStatistics(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveServiceTokenLatency("metrics:read", 20*time.Millisecond)
	registry.ObserveServiceTokenLatency("not-a-scope", time.Millisecond)
	registry.SetServiceTokenStatistics(func() (ServiceTokenStatistics, error) {
		return ServiceTokenStatistics{Active: 3, Expiring: 1, Revoked: 2}, nil
	})
	metrics := registry.Prometheus()
	for _, expected := range []string{
		`beat_service_token_operation_seconds_count{scope="metrics:read"} 1`,
		`beat_service_token_operation_seconds_bucket{scope="metrics:read",le="0.025"} 1`,
		`beat_service_tokens{state="active"} 3`,
		`beat_service_tokens{state="expiring"} 1`,
		`beat_service_tokens{state="revoked"} 2`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
	if strings.Contains(metrics, "not-a-scope") {
		t.Fatalf("unbounded latency scope retained:\n%s", metrics)
	}
}

func TestRegistryServiceTokenStatisticsKeepsLastSnapshotOnFailure(t *testing.T) {
	registry := NewRegistry()
	registry.SetServiceTokenStatistics(func() (ServiceTokenStatistics, error) {
		return ServiceTokenStatistics{Active: 5}, nil
	})
	first := registry.Prometheus()
	if !strings.Contains(first, `beat_service_tokens{state="active"} 5`) {
		t.Fatalf("statistics were not recorded:\n%s", first)
	}
	registry.SetServiceTokenStatistics(func() (ServiceTokenStatistics, error) {
		return ServiceTokenStatistics{}, errors.New("store unavailable")
	})
	second := registry.Prometheus()
	if !strings.Contains(second, `beat_service_tokens{state="active"} 5`) {
		t.Fatalf("failed scrape did not keep the last snapshot:\n%s", second)
	}
}
