package runtimediag

import (
	"errors"
	"testing"
)

func TestSummaryReportsBoundedRuntimeState(t *testing.T) {
	service := newService(t, payloadCapture("x"), func(config *Config) {
		config.Version, config.Commit = "0.2.0-test", "commit"
		config.Readiness = func() error { return errors.New("storage offline") }
	})
	summary := service.Summary()
	if summary.Version != "0.2.0-test" || summary.Commit != "commit" || summary.GoVersion == "" {
		t.Fatalf("summary identity: %+v", summary)
	}
	if summary.Readiness != "degraded" || summary.Scheduler.Goroutines < 1 || summary.Scheduler.GOMAXPROCS < 1 {
		t.Fatalf("summary runtime state: %+v", summary)
	}
	if summary.Capture.Active != 0 || summary.Capture.Busy || summary.Capture.Degraded {
		t.Fatalf("summary capture state: %+v", summary.Capture)
	}
	if summary.Memory.SystemBytes == 0 || summary.UptimeSeconds < 0 {
		t.Fatalf("summary memory state: %+v", summary)
	}
	healthy := newService(t, payloadCapture("x"), nil)
	if healthy.Summary().Readiness != "ok" {
		t.Fatal("default readiness is not ok")
	}
}
