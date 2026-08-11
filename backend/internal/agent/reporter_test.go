package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (errorReader) Close() error { return nil }

func TestHTTPReporter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes/report" || r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Fatalf("request path = %q, auth = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var report NodeReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Fatalf("decode report: %v", err)
		}
		if report.Name != "node-one" || report.Metrics.CPU != 42 ||
			report.Metrics.CPUUsed != 3.36 || report.Metrics.CPUTotal != 8 ||
			report.Metrics.MemoryUsed != 400 || report.Metrics.MemoryTotal != 800 ||
			report.Metrics.DiskUsed != 40 || report.Metrics.DiskTotal != 100 ||
			report.System.OS != "linux" || report.System.AgentVersion != "test" {
			t.Fatalf("report = %#v", report)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	reporter, err := NewHTTPReporter(Config{
		ServerURL: server.URL, AgentToken: "agent-secret", NodeName: "node-one",
		AdvertisedHost: "127.0.0.1", SSHPort: 22,
	})
	if err != nil {
		t.Fatalf("create reporter: %v", err)
	}
	metrics := model.NodeMetrics{
		SystemInfo: model.SystemInfo{OS: "linux", AgentVersion: "test"},
		CPU:        42, CPUUsed: 3.36, CPUTotal: 8,
		MemoryUsed: 400, MemoryTotal: 800,
		DiskUsed: 40, DiskTotal: 100,
	}
	if err := reporter.Report(context.Background(), metrics); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestHTTPReporterErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rejected", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	reporter, err := NewHTTPReporter(Config{
		ServerURL: server.URL, AgentToken: "token", NodeName: "node",
		AdvertisedHost: "127.0.0.1", SSHPort: 22,
	})
	if err != nil {
		t.Fatalf("create reporter: %v", err)
	}
	if err := reporter.Report(context.Background(), model.NodeMetrics{}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want status error", err)
	}
	if _, err := NewHTTPReporter(Config{ServerURL: "://invalid"}); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestHTTPReporterTransportAndBodyErrors(t *testing.T) {
	reporter, err := NewHTTPReporter(Config{
		ServerURL: "http://example.com/base?secret=removed#fragment", AgentToken: "token",
		NodeName: "node", AdvertisedHost: "127.0.0.1", SSHPort: 22,
	})
	if err != nil {
		t.Fatalf("create reporter: %v", err)
	}
	if reporter.endpoint != "http://example.com/base/api/v1/nodes/report" {
		t.Fatalf("endpoint = %q", reporter.endpoint)
	}
	reporter.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("send failed")
	})
	if err := reporter.Report(context.Background(), model.NodeMetrics{}); err == nil || !strings.Contains(err.Error(), "send") {
		t.Fatalf("error = %v, want send error", err)
	}

	tests := []struct {
		name       string
		statusCode int
		want       string
	}{
		{name: "error body", statusCode: http.StatusBadRequest, want: "unreadable body"},
		{name: "success body", statusCode: http.StatusOK, want: "read node report response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reporter.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode, Body: errorReader{}, Header: make(http.Header),
				}, nil
			})
			err := reporter.Report(context.Background(), model.NodeMetrics{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
	_ = io.Discard
}
