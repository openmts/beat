package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type erasureRestoreFixture struct {
	store *MetricErasureStore
	path  string
	now   time.Time
}

func newErasureRestoreFixture(t *testing.T) erasureRestoreFixture {
	t.Helper()
	sqlite := setupTestDB(t)
	t.Cleanup(func() { _ = sqlite.Close() })
	return erasureRestoreFixture{store: NewMetricErasureStore(sqlite.DB), path: sqlite.path, now: model.NowUTC()}
}

func seedRestoreJob(t *testing.T, fixture erasureRestoreFixture, id, scope, targetType, targetID,
	state, cursor string, attempts int) {
	t.Helper()
	if _, err := fixture.store.db.ExecContext(t.Context(), `INSERT INTO metric_erasure_jobs
		(id, scope, target_type, target_id, target_name, state, measurement_cursor, attempt_count,
		 lease_owner, lease_expires_at, request_id, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'worker-a', ?, ?, ?)`,
		id, scope, targetType, targetID, "target-"+id, state, cursor, attempts,
		fixture.now.Add(-time.Minute), id, fixture.now); err != nil {
		t.Fatalf("seed erasure job %s: %v", id, err)
	}
}

func TestReconcileMetricErasureRestoreRequeuesInFlightJobs(t *testing.T) {
	fixture := newErasureRestoreFixture(t)
	seedRestoreJob(t, fixture, "11111111-1111-4111-8111-111111111111", model.MetricErasureNodeHistory,
		model.MetricErasureTargetNode, erasureNodeID, model.MetricErasureDeleting, "cpu", 2)
	seedRestoreJob(t, fixture, "22222222-2222-4222-8222-222222222222", model.MetricErasureFleetTelemetry,
		model.MetricErasureTargetNone, "", model.MetricErasureClaimed, "", MaximumMetricErasureAttempts)
	seedRestoreJob(t, fixture, "33333333-3333-4333-8333-333333333333", model.MetricErasureAllMetricHistory,
		model.MetricErasureTargetNone, "", model.MetricErasureQueued, "memory", MaximumMetricErasureAttempts)
	seedRestoreJob(t, fixture, "44444444-4444-4444-8444-444444444444", model.MetricErasureDeleteNode,
		model.MetricErasureTargetNode, erasureNodeID, model.MetricErasureSucceeded, "network_probe", 1)

	if err := ReconcileMetricErasureRestore(t.Context(), fixture.path, fixture.now); err != nil {
		t.Fatalf("reconcile metric erasure restore: %v", err)
	}

	resumed, err := fixture.store.Get(t.Context(), "11111111-1111-4111-8111-111111111111")
	if err != nil || resumed == nil {
		t.Fatalf("read resumed job = %v, err = %v", resumed, err)
	}
	if resumed.State != model.MetricErasureQueued || resumed.MeasurementCursor != "cpu" ||
		resumed.LeaseOwner != "" || resumed.LeaseExpiresAt != nil || resumed.AttemptCount != 2 {
		t.Fatalf("resumed job = %+v", resumed)
	}

	for _, id := range []string{"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"} {
		exhausted, err := fixture.store.Get(t.Context(), id)
		if err != nil || exhausted == nil {
			t.Fatalf("read exhausted job %s = %v, err = %v", id, exhausted, err)
		}
		if exhausted.State != model.MetricErasureFailed ||
			exhausted.LastErrorCode != model.MetricErasureErrorLease || exhausted.CompletedAt == nil {
			t.Fatalf("exhausted job %s = %+v", id, exhausted)
		}
	}

	finished, err := fixture.store.Get(t.Context(), "44444444-4444-4444-8444-444444444444")
	if err != nil || finished == nil || finished.State != model.MetricErasureSucceeded {
		t.Fatalf("terminal job = %+v, err = %v", finished, err)
	}
}

func TestReconcileMetricErasureRestoreWithoutLedgerIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create plain SQLite file: %v", err)
	}
	if err := ReconcileMetricErasureRestore(t.Context(), path, model.NowUTC()); err != nil {
		t.Fatalf("reconcile without ledger = %v", err)
	}
}
