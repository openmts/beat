package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	erasureNodeID  = "22222222-2222-4222-8222-222222222222"
	erasureGroupID = "44444444-4444-4444-8444-444444444444"
	erasureTaskID  = "33333333-3333-4333-8333-333333333333"
	erasureDigest  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func metricErasureFixture(t *testing.T) *MetricErasureStore {
	t.Helper()
	sqlite := setupTestDB(t)
	t.Cleanup(func() { _ = sqlite.Close() })
	now := time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC)
	if _, err := sqlite.DB.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		erasureGroupID, "edge", now, now); err != nil {
		t.Fatalf("insert group: %v", err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(),
		`INSERT INTO nodes (id, name, alias, group_id, host, port, ssh_public_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		erasureNodeID, "server-one", "Primary", erasureGroupID, "192.0.2.1", 22, "", now, now); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(),
		`INSERT INTO network_tasks (id, name, type, target, ip_family, interval_seconds,
			timeout_milliseconds, all_nodes, enabled, is_public, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		erasureTaskID, "Probe", "tcp", "192.0.2.9:443", "auto", 60, 3000, 1, 1, 0, 0, now, now); err != nil {
		t.Fatalf("insert network task: %v", err)
	}
	return NewMetricErasureStore(sqlite.DB)
}

func metricErasureRequest(requestID, scope, target, confirmation string) model.MetricErasureCreateRequest {
	return model.MetricErasureCreateRequest{Scope: scope, TargetID: target,
		RequestID: requestID, Confirmation: confirmation}
}

func createMetricErasureJob(t *testing.T, store *MetricErasureStore, requestID, scope, target,
	confirmation string, now time.Time) model.MetricErasureJob {
	t.Helper()
	job, replayed, err := store.Create(t.Context(), MetricErasureCreate{
		Request: metricErasureRequest(requestID, scope, target, confirmation), Now: now})
	if err != nil || replayed {
		t.Fatalf("create erasure job = %v, replayed = %v, err = %v", job.State, replayed, err)
	}
	return job
}

func TestMetricErasureCreateReplaysAndRejectsConflicts(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	request := metricErasureRequest("11111111-1111-4111-8111-111111111111",
		model.MetricErasureNodeHistory, erasureNodeID, "server-one")

	job, replayed, err := store.Create(t.Context(), MetricErasureCreate{Request: request, Now: now})
	if err != nil || replayed {
		t.Fatalf("first create = %v/%v/%v", job.ID, replayed, err)
	}
	if job.TargetName != "server-one" || job.TargetType != model.MetricErasureTargetNode {
		t.Fatalf("resolved target = %q/%q", job.TargetName, job.TargetType)
	}

	replay, replayed, err := store.Create(t.Context(), MetricErasureCreate{Request: request, Now: now.Add(time.Minute)})
	if err != nil || !replayed || replay.ID != job.ID {
		t.Fatalf("replay = %v/%v/%v", replay.ID, replayed, err)
	}

	conflicting := request
	conflicting.Scope = model.MetricErasureDeleteNode
	if _, _, err := store.Create(t.Context(), MetricErasureCreate{Request: conflicting, Now: now}); !errors.Is(err, model.ErrMetricErasureConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}

	second := metricErasureRequest("44444444-4444-4444-8444-444444444444",
		model.MetricErasureNodeHistory, erasureNodeID, "server-one")
	if _, _, err := store.Create(t.Context(), MetricErasureCreate{Request: second, Now: now}); !errors.Is(err, model.ErrMetricErasureConflict) {
		t.Fatalf("concurrent entity erasure error = %v", err)
	}
}

func TestMetricErasureDeleteNodeTombstonesAndCancelRestores(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	job := createMetricErasureJob(t, store, "55555555-5555-4555-8555-555555555555",
		model.MetricErasureDeleteNode, erasureNodeID, "server-one", now)

	nodes := NewNodeStore(store.db)
	apps, err := nodes.ListNodes(t.Context(), "")
	if err != nil || len(apps) != 0 {
		t.Fatalf("tombstoned node still listed = %v, err = %v", len(apps), err)
	}
	hidden, err := nodes.GetNode(t.Context(), erasureNodeID)
	if err != nil || hidden != nil {
		t.Fatalf("tombstoned node readable = %v, err = %v", hidden, err)
	}

	if err := store.Cancel(t.Context(), job.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("cancel erasure = %v", err)
	}
	canceled, err := store.Get(t.Context(), job.ID)
	if err != nil || canceled == nil || canceled.State != model.MetricErasureCanceled {
		t.Fatalf("canceled job = %+v, err = %v", canceled, err)
	}
	restored, err := nodes.GetNode(t.Context(), erasureNodeID)
	if err != nil || restored == nil {
		t.Fatalf("restored node = %v, err = %v", restored, err)
	}
	if err := store.Cancel(t.Context(), job.ID, now.Add(2*time.Minute)); !errors.Is(err, model.ErrMetricErasureConflict) {
		t.Fatalf("second cancel error = %v", err)
	}
}

func TestMetricErasureClaimRecoversExpiredLeaseAndKeepsCursor(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	job := createMetricErasureJob(t, store, "66666666-6666-4666-8666-666666666666",
		model.MetricErasureNodeHistory, erasureNodeID, "server-one", now)

	first, err := store.Claim(t.Context(), "worker-a", now)
	if err != nil || first == nil {
		t.Fatalf("first claim = %+v, err = %v", first, err)
	}
	if first.Job.AttemptCount != 1 || first.Job.LeaseOwner != "worker-a" {
		t.Fatalf("first claim job = %+v", first.Job)
	}
	if second, err := store.Claim(t.Context(), "worker-b", now.Add(time.Minute)); err != nil || second != nil {
		t.Fatalf("claim inside lease = %+v, err = %v", second, err)
	}

	if err := store.BeginDeleting(t.Context(), *first, erasureDigest, now.UnixNano(), now.Add(time.Second)); err != nil {
		t.Fatalf("begin deleting = %v", err)
	}
	if err := store.Advance(t.Context(), job.ID, "cpu", now.Add(2*time.Second)); err != nil {
		t.Fatalf("advance cursor = %v", err)
	}
	if err := store.Release(t.Context(), job.ID, now.Add(3*time.Second)); err != nil {
		t.Fatalf("release = %v", err)
	}

	recovered, err := store.Claim(t.Context(), "worker-b", now.Add(4*time.Minute))
	if err != nil || recovered == nil {
		t.Fatalf("claim after release = %+v, err = %v", recovered, err)
	}
	if recovered.Job.MeasurementCursor != "cpu" || recovered.Job.AttemptCount != 2 {
		t.Fatalf("resumed job = %+v", recovered.Job)
	}
	if recovered.Job.CutoffUnixNano == nil || *recovered.Job.CutoffUnixNano != now.UnixNano() {
		t.Fatalf("resumed cutoff = %v", recovered.Job.CutoffUnixNano)
	}
}

func TestMetricErasureExhaustedLeaseFailsClosed(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	job := createMetricErasureJob(t, store, "77777777-7777-4777-8777-777777777777",
		model.MetricErasureNodeHistory, erasureNodeID, "server-one", now)
	claimed, err := store.Claim(t.Context(), "worker-a", now)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err = %v", claimed, err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE metric_erasure_jobs
		SET state='deleting', attempt_count=?, lease_expires_at=? WHERE id=?`,
		MaximumMetricErasureAttempts, now.Add(-time.Minute), job.ID); err != nil {
		t.Fatalf("age claim: %v", err)
	}

	next, err := store.Claim(t.Context(), "worker-b", now)
	if err != nil || next != nil {
		t.Fatalf("claim after exhaustion = %+v, err = %v", next, err)
	}
	failed, err := store.Get(t.Context(), job.ID)
	if err != nil || failed == nil || failed.State != model.MetricErasureFailed {
		t.Fatalf("exhausted job = %+v, err = %v", failed, err)
	}
	if failed.LastErrorCode != model.MetricErasureErrorLease || failed.CompletedAt == nil {
		t.Fatalf("exhausted job detail = %+v", failed)
	}

	if err := store.Retry(t.Context(), job.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("retry = %v", err)
	}
	retried, err := store.Get(t.Context(), job.ID)
	if err != nil || retried == nil || retried.State != model.MetricErasureQueued ||
		retried.AttemptCount != 0 || retried.LastErrorCode != "" {
		t.Fatalf("retried job = %+v, err = %v", retried, err)
	}
}

func TestMetricErasureTransitionsCommandEntityTombstones(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	job := createMetricErasureJob(t, store, "88888888-8888-4888-8888-888888888888",
		model.MetricErasureDeleteNetworkTask, erasureTaskID, "Probe", now)
	claimed, err := store.Claim(t.Context(), "worker-a", now)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err = %v", claimed, err)
	}
	if err := store.BeginDeleting(t.Context(), *claimed, erasureDigest, now.UnixNano(), now); err != nil {
		t.Fatalf("begin deleting = %v", err)
	}
	if err := store.BeginCompacting(t.Context(), job.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("begin compacting = %v", err)
	}
	if err := store.BeginFinalizing(t.Context(), job.ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("begin finalizing = %v", err)
	}
	if err := store.Complete(t.Context(), job.ID, now.Add(3*time.Second)); err != nil {
		t.Fatalf("complete = %v", err)
	}
	completed, err := store.Get(t.Context(), job.ID)
	if err != nil || completed == nil || completed.State != model.MetricErasureSucceeded {
		t.Fatalf("completed job = %+v, err = %v", completed, err)
	}
	if err := store.FinalizeEntity(t.Context(), *completed, now.Add(4*time.Second)); err != nil {
		t.Fatalf("finalize entity = %v", err)
	}
	tasks := NewNetworkTaskStore(store.db)
	gone, err := tasks.GetTask(t.Context(), erasureTaskID)
	if err != nil || gone != nil {
		t.Fatalf("finalized task = %v, err = %v", gone, err)
	}
	if err := store.FinalizeEntity(t.Context(), *completed, now.Add(5*time.Second)); err != nil {
		t.Fatalf("repeated finalize = %v", err)
	}
	if err := store.Complete(t.Context(), job.ID, now.Add(6*time.Second)); !errors.Is(err, model.ErrMetricErasureConflict) {
		t.Fatalf("second complete error = %v", err)
	}
}

func TestMetricErasureStatusSeparatesActiveAndDegraded(t *testing.T) {
	store := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	job := createMetricErasureJob(t, store, "99999999-9999-4999-8999-999999999999",
		model.MetricErasureFleetTelemetry, "", model.MetricErasureFleetConfirmation, now)
	claimed, err := store.Claim(t.Context(), "worker-a", now)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err = %v", claimed, err)
	}
	if err := store.Fail(t.Context(), job.ID, model.MetricErasureErrorMTS, now.Add(time.Minute)); err != nil {
		t.Fatalf("fail job = %v", err)
	}
	status, err := store.Status(t.Context())
	if err != nil {
		t.Fatalf("status = %v", err)
	}
	if status.Active() || !status.Degraded() || status.Counts()[model.MetricErasureFailed] != 1 {
		t.Fatalf("status = %+v", status.Counts())
	}
	for _, scope := range MetricErasureActiveScopes(status) {
		if scope.Count != 0 {
			t.Fatalf("active scope %q = %d", scope.Name, scope.Count)
		}
	}
	if len(MetricErasureStates()) != 8 {
		t.Fatalf("states = %v", MetricErasureStates())
	}
}
