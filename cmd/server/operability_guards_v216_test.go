package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/store"
)

type stubSecurityMetrics struct{ err error }

func (stub stubSecurityMetrics) SecurityMetricsSnapshot() adminauth.SecurityMetricsSnapshot {
	return adminauth.SecurityMetricsSnapshot{}
}

func (stub stubSecurityMetrics) SecurityMetrics(context.Context) (store.SecurityDiagnostics, error) {
	return store.SecurityDiagnostics{}, stub.err
}

func (stub stubSecurityMetrics) SecurityOutboxAge(context.Context, time.Time) (float64, bool, error) {
	return 0, false, stub.err
}

type stubReadiness struct{ err error }

func (stub stubReadiness) Ready(context.Context) error { return stub.err }

func TestLoginSecurityCheckCoversMissingAndFailingSource(t *testing.T) {
	empty := &operability{}
	if check := empty.loginSecurityCheck(t.Context()); check.Status != "ok" {
		t.Fatalf("missing source check = %+v", check)
	}
	failing := &operability{security: stubSecurityMetrics{err: errors.New("unavailable")}}
	if check := failing.loginSecurityCheck(t.Context()); check.Status != "error" {
		t.Fatalf("failing source check = %+v", check)
	}
	healthy := &operability{security: stubSecurityMetrics{}}
	if check := healthy.loginSecurityCheck(t.Context()); check.Status != "ok" {
		t.Fatalf("healthy source check = %+v", check)
	}
	var output strings.Builder
	healthy.writeSecurityMetrics(&output, t.Context())
	if !strings.Contains(output.String(), "beat_security_login_events_total") {
		t.Fatalf("security metrics are empty: %q", output.String())
	}
}

func TestSecurityMetricsSourceRequiresAuthenticationService(t *testing.T) {
	if source := securityMetricsSourceFrom(nil); source != nil {
		t.Fatal("nil services exposed a security source")
	}
	if source := securityMetricsSourceFrom(&serverServices{}); source != nil {
		t.Fatal("services without authentication exposed a security source")
	}
	if source := securityMetricsSourceFrom(&serverServices{security: &adminauth.Service{}}); source == nil {
		t.Fatal("initialized authentication did not expose a security source")
	}
}

func TestNotificationCheckAndEnrollmentMetricsFailClosed(t *testing.T) {
	empty := &operability{}
	if check := empty.notificationCheck(t.Context()); check.Status != "error" {
		t.Fatalf("missing queue check = %+v", check)
	}
	failing := &operability{notificationQueue: stubReadiness{err: errors.New("unavailable")}}
	if check := failing.notificationCheck(t.Context()); check.Status != "error" {
		t.Fatalf("failing queue check = %+v", check)
	}
	healthy := &operability{notificationQueue: stubReadiness{}}
	if check := healthy.notificationCheck(t.Context()); check.Status != "ok" {
		t.Fatalf("healthy queue check = %+v", check)
	}

	registry := observability.NewRegistry()
	registerEnrollmentMetrics(registry, nil)
	registerEnrollmentMetrics(registry, &serverStores{})
	registerEnrollmentMetrics(nil, &serverStores{})
	fixture := newCommercialFixture(t)
	registerEnrollmentMetrics(registry, fixture.stores)
	if output := registry.Prometheus(); !strings.Contains(output, "beat_agent_enrollment_requests{status=") {
		t.Fatalf("enrollment metrics are empty: %q", output)
	}
	if err := fixture.stores.sqlite.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	registry.Prometheus()
}

func TestCombineCleanupObserverReportsCompletedAndFailed(t *testing.T) {
	registry := observability.NewRegistry()
	var observed []error
	observer := combineCleanupObserver(func(err error) { observed = append(observed, err) }, registry)
	observer(nil)
	observer(errors.New("cleanup failed"))
	if len(observed) != 2 || observed[0] != nil {
		t.Fatalf("worker observed %#v", observed)
	}
	output := registry.Prometheus()
	for _, expected := range []string{
		`beat_agent_enrollment_cleanup_total{result="completed"} 1`,
		`beat_agent_enrollment_cleanup_total{result="failed"} 1`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q in %q", expected, output)
		}
	}
}
