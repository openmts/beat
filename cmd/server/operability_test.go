package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/observability"
)

type fakeSQLiteMonitor struct {
	readyErr error
	bytes    int64
}

func (monitor fakeSQLiteMonitor) Ready(context.Context) error { return monitor.readyErr }

func (monitor fakeSQLiteMonitor) DiskUsage() (int64, error) { return monitor.bytes, nil }

type fakeMTSMonitor struct {
	healthy bool
	bytes   int64
	diskErr error
}

func (monitor fakeMTSMonitor) Health() (bool, []string) { return monitor.healthy, nil }

func (monitor fakeMTSMonitor) DiskUsage() (int64, error) { return monitor.bytes, monitor.diskErr }

type fakeNodeMonitor struct {
	nodes []model.Node
	err   error
}

func (monitor fakeNodeMonitor) ListNodes(context.Context, string) ([]model.Node, error) {
	return monitor.nodes, monitor.err
}

type fakeBackupMonitor struct {
	records []model.BackupRecord
	err     error
}

func (monitor fakeBackupMonitor) List(context.Context) ([]model.BackupRecord, error) {
	return monitor.records, monitor.err
}

type fakeRunningMonitor bool

func (monitor fakeRunningMonitor) Running() bool { return bool(monitor) }

type fakeNotificationMonitor struct{ stats notification.DeliveryStats }

func (monitor fakeNotificationMonitor) Stats() notification.DeliveryStats { return monitor.stats }

func newTestOperability() *operability {
	runtime := &runtimeState{}
	runtime.schedulers.Store(true)
	return &operability{
		sqlite: fakeSQLiteMonitor{bytes: 10}, mts: fakeMTSMonitor{healthy: true, bytes: 20},
		nodes: fakeNodeMonitor{nodes: []model.Node{{Status: model.NodeStatusOnline,
			LastSeen: time.Now().Add(-time.Minute)}}},
		backupRecords: fakeBackupMonitor{records: []model.BackupRecord{{State: model.BackupStateReady}}}, backups: fakeRunningMonitor(false), maintenance: fakeRunningMonitor(true),
		notifications: fakeNotificationMonitor{stats: notification.DeliveryStats{Success: 3, Failed: 1}},
		metrics:       observability.NewRegistry(), runtime: runtime,
		pendingRestore: func(string) (bool, error) { return false, nil }, dataDir: tTempPath,
	}
}

const tTempPath = "/test-data"

func TestOperabilityHealthAndReadiness(t *testing.T) {
	operations := newTestOperability()
	for _, endpoint := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		response := httptest.NewRecorder()
		mux := http.NewServeMux()
		operations.register(mux)
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status"`) {
			t.Fatalf("%s response = %d %s", endpoint, response.Code, response.Body.String())
		}
	}
}

func TestOperabilityReadinessFailureDoesNotLeakError(t *testing.T) {
	operations := newTestOperability()
	operations.sqlite = fakeSQLiteMonitor{readyErr: errors.New("secret database path")}
	operations.runtime.schedulers.Store(false)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	operations.handleReady(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "secret database path") {
		t.Fatalf("readiness leaked internal error: %s", response.Body.String())
	}
}

func TestPendingRestoreRemainsReady(t *testing.T) {
	operations := newTestOperability()
	operations.pendingRestore = func(string) (bool, error) { return true, nil }
	response, ready := operations.readiness(context.Background())
	if !ready || response.Checks["restore"].Detail != "restart_required" {
		t.Fatalf("readiness = %#v, ready=%v", response, ready)
	}
}

func TestOperabilityMetrics(t *testing.T) {
	operations := newTestOperability()
	metrics := operations.prometheus(context.Background())
	for _, expected := range []string{
		"beat_agents_total 1", "beat_agents_online 1", `beat_storage_bytes{store="sqlite"} 10`,
		`beat_notification_deliveries_total{result="success"} 3`, `beat_backups_total{state="ready"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
}

func TestOperabilityMetricsHandlerAndFailureGauges(t *testing.T) {
	operations := newTestOperability()
	operations.nodes = fakeNodeMonitor{err: errors.New("nodes unavailable")}
	operations.mts = fakeMTSMonitor{healthy: false, diskErr: errors.New("disk unavailable")}
	operations.backupRecords = fakeBackupMonitor{err: errors.New("backups unavailable")}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	operations.handleMetrics(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", response.Code)
	}
	for _, expected := range []string{"beat_agents_query_success 0", `beat_storage_bytes{store="mts"} 0`,
		`beat_readiness_check{check="mts"} 0`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, response.Body.String())
		}
	}
}

func TestOperabilityRestoreCheckFailure(t *testing.T) {
	operations := newTestOperability()
	operations.pendingRestore = func(string) (bool, error) { return false, errors.New("invalid journal") }
	check := operations.restoreCheck()
	if check.Status != "error" || check.Detail != "invalid" {
		t.Fatalf("restore check = %#v", check)
	}
}
