package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// failingResponseWriter 让状态响应在写正文时失败，用于覆盖日志回落分支。
type failingResponseWriter struct{ header http.Header }

func (writer failingResponseWriter) Header() http.Header {
	if writer.header == nil {
		return http.Header{}
	}
	return writer.header
}

func (failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func (failingResponseWriter) WriteHeader(int) {}

func TestPrepareDataDirectoryFailsClosedOnUnusableState(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	if _, err := prepareDataDirectory(t.Context(),
		filepath.Join(blocked, "beat.db"), filepath.Join(root, "mts")); err == nil {
		t.Fatal("data directory below a regular file was accepted")
	}

	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, ".root-key-rotation.prepare"), 0o700); err != nil {
		t.Fatalf("create unfinished rotation marker: %v", err)
	}
	if _, err := prepareDataDirectory(t.Context(),
		filepath.Join(dataDir, "beat.db"), filepath.Join(dataDir, "mts")); err == nil {
		t.Fatal("unfinished root key rotation was accepted")
	}
}

func TestSchedulerChecksReportStoppedHealthyAndFailedWorkers(t *testing.T) {
	empty := &operability{runtime: newRuntimeState()}
	if check := empty.schedulerCheck(); check.Status != "error" || check.Detail != "stopped" {
		t.Fatalf("empty scheduler check = %+v", check)
	}
	empty.runtime.workers.register("alerts", time.Minute)
	if check := empty.schedulerCheck(); check.Status != "error" {
		t.Fatalf("stopped worker check = %+v", check)
	}

	healthyState := newRuntimeState()
	healthy := healthyState.workers.register("alerts", time.Minute)
	healthy.running.Store(true)
	healthy.lastSuccess.Store(time.Now().UnixNano())
	operations := &operability{runtime: healthyState}
	if check := operations.schedulerCheck(); check.Status != "ok" {
		t.Fatalf("healthy scheduler check = %+v", check)
	}

	operations.runtime.workers.register("mystery_worker", time.Minute)
	failed := operations.runtime.workers.register("metric_erasure", time.Minute)
	failed.failed.Store(true)
	checks := map[string]readinessCheck{}
	operations.addWorkerChecks(checks)
	if check := checks["worker_metric_erasure"]; check.Status != "error" || check.Detail != "unavailable" {
		t.Fatalf("failed worker check = %+v", check)
	}
	if _, found := checks["worker_mystery_worker"]; found {
		t.Fatal("unknown worker was reported in readiness checks")
	}
}

func TestWriteStatusJSONLogsWriteFailure(t *testing.T) {
	writeStatusJSON(failingResponseWriter{}, http.StatusServiceUnavailable,
		map[string]string{"status": "not_ready"})
}

func TestInitializeCommercialFailsClosedOnRelativeGeoIPPath(t *testing.T) {
	fixture := newCommercialFixture(t)
	t.Setenv("BEAT_GEOIP_MMDB_PATH", "relative/country.mmdb")
	if err := fixture.services.initializeNodeLifecycle(t.Context(), fixture.stores, fixture.directory); err == nil {
		t.Fatal("relative GeoIP path was accepted by the node lifecycle initializer")
	}
	if err := fixture.services.initializeCommercial(t.Context(), fixture.stores, fixture.directory); err == nil {
		t.Fatal("relative GeoIP path was accepted by the commercial initializer")
	}
}
