package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestRunnerCollectsAndReportsRegisteredCustomMetrics(t *testing.T) {
	received := make(chan NodeReport, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var report NodeReport
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Error(err)
			return
		}
		select {
		case received <- report:
		case <-request.Context().Done():
			return
		}
		writeTestReportACK(t, w, testACKFixture{report: report})
	}))
	t.Cleanup(server.Close)
	reporter, err := NewHTTPReporter(Config{ServerURL: server.URL, AgentToken: "test-token", NodeName: "joined-node", AdvertisedHost: "192.0.2.8", SSHPort: 22})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewCustomMetricRegistry()
	replaceCustomRegistry(t, registry, []model.CustomMetric{{Name: "custom_queue", ShellCommand: "printf 7", IntervalSeconds: 5, Enabled: true}})
	ctx, cancel := context.WithCancel(t.Context())
	runner := NewRunner(RunnerOptions{Interval: 10 * time.Millisecond, Collector: &fakeCollector{metrics: model.NodeMetrics{CPU: 12.5}},
		CustomSampler: NewCustomMetricCollector(registry), Reporter: reporter})
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	deadline := time.After(time.Second)
	for {
		select {
		case report := <-received:
			if hasRegisteredCustomSample(t, report) {
				return
			}
		case <-deadline:
			t.Fatal("registered custom metric was not reported")
		}
	}
}

func hasRegisteredCustomSample(t *testing.T, report NodeReport) bool {
	t.Helper()
	if report.Name != "joined-node" || report.Metrics.CPU != 12.5 {
		t.Fatal("system report was changed")
	}
	value, ok := report.CustomMetrics["custom_queue"]
	if ok && value != 7 {
		t.Fatal("custom value changed")
	}
	return ok
}

func TestHTTPReporterRejectsNonFiniteMeasurementsBeforeSending(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	reporter, err := NewHTTPReporter(Config{ServerURL: server.URL, AgentToken: "test-token", NodeName: "joined-node", AdvertisedHost: "192.0.2.8", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := reporter.Report(context.Background(), testReportSample(t, model.NodeMetrics{}, map[string]float64{"custom_queue": value})); err == nil || !strings.Contains(err.Error(), "invalid_report") {
			t.Fatalf("expected non-finite measurement rejection, got %v", err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid measurements reached the server")
	}
}

func TestRunnerContinuesAfterPeriodicReportFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transient := errors.New("temporary report failure")
	calls := 0
	reporter := runtimeReporterFunc(func(context.Context, ReportSample) (string, error) {
		calls++
		if calls == 2 {
			return "", transient
		}
		if calls >= 3 {
			cancel()
		}
		return "", nil
	})
	var seen []error
	runner := NewRunner(RunnerOptions{Interval: time.Millisecond, Collector: &fakeCollector{}, Reporter: reporter,
		OnError: func(err error) { seen = append(seen, err) }})
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if calls < 3 || len(seen) != 1 || !errors.Is(seen[0], transient) {
		t.Fatalf("report loop did not recover: calls=%d, errors=%v", calls, seen)
	}
}

type runtimeReporterFunc func(context.Context, ReportSample) (string, error)

func (report runtimeReporterFunc) Report(ctx context.Context, sample ReportSample) (string, error) {
	return report(ctx, sample)
}

func TestAssignedKeyHealingPreservesConflictingFilesystemEntries(t *testing.T) {
	for _, kind := range []string{"ssh directory is a file", "authorized keys is a directory"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			sshPath := filepath.Join(directory, ".ssh")
			conflict := sshPath
			if kind == "ssh directory is a file" {
				if err := os.WriteFile(conflict, []byte("existing data"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				conflict = filepath.Join(sshPath, "authorized_keys")
				if err := os.MkdirAll(conflict, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := NewAuthorizedKeySelfHealer(directory).SelfHeal(context.Background(), "ssh-ed25519 new-key"); err == nil {
				t.Fatal("expected filesystem conflict")
			}
			info, err := os.Stat(conflict)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "authorized keys is a directory" && !info.IsDir() {
				t.Fatal("conflicting directory was replaced")
			}
			if kind == "ssh directory is a file" {
				contents, err := os.ReadFile(conflict)
				if err != nil || string(contents) != "existing data" {
					t.Fatalf("conflicting file changed: %q, %v", contents, err)
				}
			}
		})
	}
}

func TestAssignedKeyHealingPreservesExistingKeyWithoutTrailingNewline(t *testing.T) {
	directory := t.TempDir()
	sshPath := filepath.Join(directory, ".ssh")
	if err := os.Mkdir(sshPath, 0o700); err != nil {
		t.Fatal(err)
	}
	keysPath := filepath.Join(sshPath, "authorized_keys")
	if err := os.WriteFile(keysPath, []byte("ssh-ed25519 old-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewAuthorizedKeySelfHealer(directory).SelfHeal(context.Background(), "ssh-ed25519 new-key"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(keysPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "ssh-ed25519 old-key\nssh-ed25519 new-key\n" {
		t.Fatalf("existing key was not preserved: %q", contents)
	}
}
