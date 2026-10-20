package runtimediag

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"testing"
	"time"
)

func errorCapture(err error) CaptureFunc {
	return func(context.Context, string, time.Duration, io.Writer) error { return err }
}

func artifactPath(t *testing.T, service *Service) string {
	t.Helper()
	entries, err := os.ReadDir(service.config.Directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifact directory = %v %v", entries, err)
	}
	return filepath.Join(service.config.Directory, entries[0].Name())
}

func TestAdmissionRejectsCapacityAndDegradedState(t *testing.T) {
	service := newService(t, blockingCapture, nil)
	service.mu.Lock()
	service.cleanupFailures = 1
	service.mu.Unlock()
	if _, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0); CodeOf(err) != CodeCapacity {
		t.Fatalf("degraded service admitted a capture: %v", err)
	}
	service.mu.Lock()
	service.cleanupFailures = 0
	service.mu.Unlock()

	for index := range 4 {
		owner := fmt.Sprintf("owner-%d", index)
		if _, err := service.Start(t.Context(), owner, "session", KindGoroutine, 0); err != nil {
			t.Fatalf("capture %d was rejected: %v", index, err)
		}
	}
	if _, err := service.Start(t.Context(), "owner-extra", "session", KindGoroutine, 0); CodeOf(err) != CodeCapacity {
		t.Fatalf("fifth capture error = %v", err)
	}
}

func TestAdmissionCountsDirectoryBytes(t *testing.T) {
	service := newService(t, payloadCapture("x"), nil)
	if err := os.RemoveAll(service.config.Directory); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0); CodeOf(err) != CodeCapacity {
		t.Fatalf("unreadable directory error = %v", err)
	}
}

func TestCaptureStartupFailuresPropagate(t *testing.T) {
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Fatal(err)
	}
	err := captureCPU(t.Context(), 10*time.Millisecond, io.Discard)
	pprof.StopCPUProfile()
	if err == nil {
		t.Fatal("second CPU profiler start succeeded")
	}
	if err := trace.Start(io.Discard); err != nil {
		t.Fatal(err)
	}
	err = captureTrace(t.Context(), time.Millisecond, io.Discard)
	trace.Stop()
	if err == nil {
		t.Fatal("second trace start succeeded")
	}
	if err := captureInstant("runtime-diagnostics-missing", io.Discard); err == nil {
		t.Fatal("unknown instantaneous profile succeeded")
	}
}

func TestDownloadRejectsConflictsAndMissingArtifacts(t *testing.T) {
	service := newService(t, payloadCapture("payload"), nil)
	capture, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0)
	if err != nil {
		t.Fatal(err)
	}
	complete := waitForStatus(t, service, "owner", StatusComplete)
	if complete.ID != capture.ID {
		t.Fatalf("capture identity changed: %+v", complete)
	}
	if _, _, err := service.Download("owner", "missing"); CodeOf(err) != CodeNotFound {
		t.Fatalf("unknown capture error = %v", err)
	}
	running, err := service.Start(t.Context(), "owner", "session", KindCPU, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Download("owner", running.ID); CodeOf(err) != CodeConflict {
		t.Fatalf("running capture download error = %v", err)
	}
	if err := service.Cancel("owner", running.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifactPath(t, service)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Download("owner", complete.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("missing artifact error = %v", err)
	}
	service.consume("missing")
	found := false
	for _, listed := range service.List("owner") {
		found = found || listed.ID == complete.ID
	}
	if !found {
		t.Fatalf("capture metadata disappeared: %+v", service.List("owner"))
	}
}

func TestDownloadConsumptionReleasesAndConsumesArtifacts(t *testing.T) {
	service := newService(t, payloadCapture("payload"), nil)
	capture, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, service, "owner", StatusComplete)
	reader, metadata, err := service.Download("owner", capture.ID)
	if err != nil || metadata.Bytes == 0 {
		t.Fatalf("first download = %+v %v", metadata, err)
	}
	if _, err := reader.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reader, _, err = service.Download("owner", capture.ID)
	if err != nil {
		t.Fatalf("interrupted download was not released: %v", err)
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel("owner", capture.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("consumed capture cancel error = %v", err)
	}
}

func TestCancelReportsTerminalStatesWithoutWaiting(t *testing.T) {
	service := newService(t, errorCapture(errors.New("capture failed")), nil)
	capture, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForStatus(t, service, "owner", StatusFailed)
	if failed.ID != capture.ID {
		t.Fatalf("failed capture = %+v", failed)
	}
	if err := service.Cancel("owner", capture.ID); err != nil {
		t.Fatalf("cancel of a failed capture = %v", err)
	}
	if captures := service.List("owner"); len(captures) != 1 || captures[0].Status != StatusFailed {
		t.Fatalf("failed metadata changed: %+v", captures)
	}
	if err := service.Cancel("owner", "missing"); CodeOf(err) != CodeNotFound {
		t.Fatalf("cancel of an unknown capture error = %v", err)
	}
}

func TestSweepCancelsOverdueRunningCapture(t *testing.T) {
	service := newService(t, blockingCapture, nil)
	capture, err := service.Start(t.Context(), "owner", "session", KindCPU, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	item := service.items[capture.ID]
	service.mu.Unlock()
	service.now = func() time.Time { return item.Deadline.Add(service.limits.MaximumDuration + time.Second) }
	service.Sweep()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := service.List("owner")
		if len(status) == 1 && status[0].Status == StatusCancelled {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("overdue capture was not cancelled: %+v", service.List("owner"))
}

func TestCloseReportsArtifactCleanupFailures(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	service, err := New(Config{Directory: directory, Capture: payloadCapture("payload")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(t.Context(), "owner", "session", KindGoroutine, 0); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, service, "owner", StatusComplete)
	path := artifactPath(t, service)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err == nil {
		t.Fatal("close hid the cleanup failure")
	}
	if service.Health() == nil {
		t.Fatal("cleanup failure did not degrade health")
	}
}

func TestReadinessAndPauseSummaryEdgeCases(t *testing.T) {
	healthy := newService(t, payloadCapture("x"), func(config *Config) {
		config.Readiness = func() error { return nil }
	})
	if summary := healthy.Summary(); summary.Readiness != "ok" {
		t.Fatalf("readiness = %q", summary.Readiness)
	}
	if seconds := lastPauseSeconds(runtime.MemStats{}); seconds != 0 {
		t.Fatalf("pause seconds for an idle runtime = %v", seconds)
	}
	if code := CodeOf(errors.New("unrelated failure")); code != CodeWriterFailed {
		t.Fatalf("unknown error code = %q", code)
	}
	if code := CodeOf(nil); code != "" {
		t.Fatalf("nil error code = %q", code)
	}
}
