package runtimediag

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func blockingCapture(ctx context.Context, _ string, _ time.Duration, _ io.Writer) error {
	<-ctx.Done()
	return ctx.Err()
}

func payloadCapture(payload string) CaptureFunc {
	return func(_ context.Context, _ string, _ time.Duration, writer io.Writer) error {
		_, err := writer.Write([]byte(payload))
		return err
	}
}

func newService(t *testing.T, capture CaptureFunc, mutate func(*Config)) *Service {
	t.Helper()
	config := Config{Directory: filepath.Join(t.TempDir(), "diagnostics"), Capture: capture, Version: "0.2.0-test", Commit: "commit"}
	if mutate != nil {
		mutate(&config)
	}
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func waitForStatus(t *testing.T, service *Service, owner, want string) Capture {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, capture := range service.List(owner) {
			if capture.Status == want {
				return capture
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("capture never reached %s: %+v", want, service.List(owner))
	return Capture{}
}

func TestStartValidatesKindAndDuration(t *testing.T) {
	service := newService(t, payloadCapture("x"), nil)
	for name, input := range map[string]struct {
		kind     string
		duration time.Duration
		code     string
	}{
		"unknown kind":      {"pprof-anything", 0, CodeInvalidKind},
		"instantaneous":     {KindGoroutine, time.Second, CodeInvalidDuration},
		"short cpu":         {KindCPU, 2 * time.Second, CodeInvalidDuration},
		"long trace":        {KindTrace, 6 * time.Second, CodeInvalidDuration},
		"long cpu":          {KindCPU, 31 * time.Second, CodeInvalidDuration},
		"short trace":       {KindTrace, 0, CodeInvalidDuration},
		"unsupported blank": {"", 0, CodeInvalidKind},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Start(context.Background(), "owner", "session", input.kind, input.duration); CodeOf(err) != input.code {
				t.Fatalf("code: %v", err)
			}
		})
	}
}

func TestConcurrencyAndRateLimits(t *testing.T) {
	service := newService(t, blockingCapture, nil)
	if _, err := service.Start(context.Background(), "owner", "session", KindCPU, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), "owner", "session", KindTrace, time.Second); CodeOf(err) != CodeConflict {
		t.Fatalf("timed conflict: %v", err)
	}
	fast := newService(t, payloadCapture("instant"), nil)
	for index := 0; index < instantaneousPerMinute; index++ {
		if _, err := fast.Start(context.Background(), "owner", "session", KindHeap, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fast.Start(context.Background(), "owner", "session", KindHeap, 0); CodeOf(err) != CodeRateLimited {
		t.Fatalf("rate limit: %v", err)
	}
	if _, err := fast.Start(context.Background(), "other", "session", KindHeap, 0); err != nil {
		t.Fatalf("other owner rejected: %v", err)
	}
}

func TestCaptureCompletesAndConsumesOnce(t *testing.T) {
	service := newService(t, payloadCapture("profile-bytes"), nil)
	created, err := service.Start(context.Background(), "owner", "session", KindGoroutine, 0)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != StatusRunning || created.Kind != KindGoroutine {
		t.Fatalf("created: %+v", created)
	}
	completed := waitForStatus(t, service, "owner", StatusComplete)
	if completed.Bytes != int64(len("profile-bytes")) {
		t.Fatalf("bytes: %+v", completed)
	}
	reader, metadata, err := service.Download("owner", completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(content) != "profile-bytes" || metadata.ID != completed.ID {
		t.Fatalf("download: %q %+v", string(content), metadata)
	}
	if items := service.List("owner"); len(items) != 0 {
		t.Fatalf("consumed capture still listed: %+v", items)
	}
	if _, _, err := service.Download("owner", completed.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("second download: %v", err)
	}
	if _, _, err := service.Download("stranger", completed.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("cross-owner download: %v", err)
	}
}

func TestSizeLimitFailsCaptureAndRemovesFile(t *testing.T) {
	service := newService(t, payloadCapture(strings.Repeat("a", 64)), func(config *Config) {
		config.Limits = DefaultLimits()
		config.Limits.MaximumArtifactBytes = 32
		config.Limits.MaximumDirectoryBytes = 64
	})
	if _, err := service.Start(context.Background(), "owner", "session", KindHeap, 0); err != nil {
		t.Fatal(err)
	}
	failed := waitForStatus(t, service, "owner", StatusFailed)
	if failed.FailureCode != CodeSizeExceeded || failed.Bytes != 0 {
		t.Fatalf("failed capture: %+v", failed)
	}
	entries, err := os.ReadDir(service.config.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("artifact not removed: %+v", entries)
	}
}

func TestCancelAndSessionRevocation(t *testing.T) {
	service := newService(t, blockingCapture, nil)
	running, err := service.Start(context.Background(), "owner", "session-a", KindCPU, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel("stranger", running.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("cross-owner cancel: %v", err)
	}
	if err := service.Cancel("owner", running.ID); err != nil {
		t.Fatal(err)
	}
	cancelled := waitForStatus(t, service, "owner", StatusCancelled)
	if cancelled.FailureCode != "" {
		t.Fatalf("cancelled capture: %+v", cancelled)
	}
	second, err := service.Start(context.Background(), "owner", "session-b", KindCPU, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service.RevokeSession("session-b")
	if items := service.List("owner"); len(items) != 1 || items[0].ID != cancelled.ID {
		t.Fatalf("revoked session still listed: %+v", items)
	}
	if err := service.Cancel("owner", second.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("removed capture cancel: %v", err)
	}
}

func TestExpirySweepAndStartupCleanup(t *testing.T) {
	service := newService(t, payloadCapture("expire-me"), func(config *Config) {
		config.Limits = DefaultLimits()
		config.Limits.Expiry = 50 * time.Millisecond
	})
	if _, err := service.Start(context.Background(), "owner", "session", KindHeap, 0); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, service, "owner", StatusComplete)
	time.Sleep(60 * time.Millisecond)
	if removed := service.Sweep(); removed != 1 {
		t.Fatalf("sweep removed %d", removed)
	}
	if items := service.List("owner"); len(items) != 0 {
		t.Fatalf("expired capture listed: %+v", items)
	}
	directory := filepath.Join(t.TempDir(), "diagnostics")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(directory, "capture-orphan.prof")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(Config{Directory: directory, Capture: payloadCapture("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan artifact survived startup")
	}
	nested := filepath.Join(directory, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Directory: directory, Capture: payloadCapture("x")}); err == nil {
		t.Fatal("unexpected directory entry accepted")
	}
}

func TestCloseStopsRunningCapturesAndClearsArtifacts(t *testing.T) {
	service := newService(t, blockingCapture, nil)
	if _, err := service.Start(context.Background(), "owner", "session", KindCPU, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if !service.Busy() {
		t.Fatal("capture not marked busy")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(service.config.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("artifacts left behind: %+v", entries)
	}
	if _, err := service.Start(context.Background(), "owner", "session", KindHeap, 0); CodeOf(err) != CodeCapacity {
		t.Fatalf("closed service accepted capture: %v", err)
	}
}

func TestLimitsAndConfigurationValidation(t *testing.T) {
	for name, limits := range map[string]Limits{
		"zero":        {},
		"cpu range":   {MinimumDuration: time.Second, MaximumDuration: time.Second, MinimumCPUDuration: 2 * time.Second, MaximumTraceDuration: time.Second, MaximumArtifactBytes: 1, MaximumDirectoryBytes: 1, Expiry: time.Minute},
		"trace range": {MinimumDuration: time.Second, MaximumDuration: 30 * time.Second, MinimumCPUDuration: time.Second, MaximumTraceDuration: 31 * time.Second, MaximumArtifactBytes: 1, MaximumDirectoryBytes: 1, Expiry: time.Minute},
		"bytes":       {MinimumDuration: time.Second, MaximumDuration: time.Second, MinimumCPUDuration: time.Second, MaximumTraceDuration: time.Second, MaximumArtifactBytes: 8, MaximumDirectoryBytes: 1, Expiry: time.Minute},
		"expiry":      {MinimumDuration: time.Second, MaximumDuration: time.Second, MinimumCPUDuration: time.Second, MaximumTraceDuration: time.Second, MaximumArtifactBytes: 1, MaximumDirectoryBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := limits.Validate(); err == nil {
				t.Fatalf("invalid limits accepted: %+v", limits)
			}
		})
	}
	if err := DefaultLimits().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty config accepted")
	}
	if _, err := New(Config{Directory: t.TempDir()}); err == nil {
		t.Fatal("missing capture function accepted")
	}
	if CodeOf(nil) != "" || CodeOf(context.Canceled) != CodeWriterFailed {
		t.Fatal("code classification is wrong")
	}
}
