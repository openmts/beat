package metricserasure

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type erasureFixture struct {
	sqlite  *store.SQLiteStore
	mts     *store.MTSStore
	jobs    *store.MetricErasureStore
	service *Service
	ctx     interface {
		Deadline() (time.Time, bool)
		Done() <-chan struct{}
		Err() error
		Value(any) any
	}
	nodeID string
	now    time.Time
}

func newErasureFixture(t *testing.T) erasureFixture {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	mts, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mts.Close() })
	now := model.NowUTC()
	if _, err := sqlite.DB.ExecContext(t.Context(),
		"INSERT INTO nodes (id,name,host,port,created_at,updated_at) VALUES (?,?,?,?,?,?)",
		"node-1", "erasure-node", "192.0.2.9", 22, now, now); err != nil {
		t.Fatal(err)
	}
	jobs := store.NewMetricErasureStore(sqlite.DB)
	return erasureFixture{sqlite: sqlite, mts: mts, jobs: jobs,
		service: NewService(jobs, mts), ctx: nil, nodeID: "node-1", now: now}
}

func (fixture erasureFixture) create(t *testing.T, scope, target, confirmation string) model.MetricErasureJob {
	t.Helper()
	job, replayed, err := fixture.jobs.Create(t.Context(), store.MetricErasureCreate{
		Request: model.MetricErasureCreateRequest{Scope: scope, TargetID: target,
			RequestID: "aaaaaaaa-1111-1111-1111-111111111111", Confirmation: confirmation},
		Now: model.NowUTC()})
	if err != nil || replayed {
		t.Fatalf("create erasure job = %#v %v %v", job, replayed, err)
	}
	return job
}

// drain 领取并执行一个任务，返回终态与 worker 报告的失败（如有）。
func (fixture erasureFixture) drain(t *testing.T) (model.MetricErasureJob, error) {
	t.Helper()
	claim, err := fixture.jobs.Claim(t.Context(), "worker-1", model.NowUTC())
	if err != nil || claim == nil {
		t.Fatalf("claim erasure job = %#v %v", claim, err)
	}
	processErr := fixture.service.Process(t.Context(), *claim)
	job, err := fixture.jobs.Get(t.Context(), claim.Job.ID)
	if err != nil || job == nil {
		t.Fatalf("read erasure job = %#v %v", job, err)
	}
	return *job, processErr
}

func metricTotal(t *testing.T, mts *store.MTSStore, nodeID, metric string) float64 {
	t.Helper()
	from := model.NowUTC().Add(-6 * time.Hour)
	values, err := mts.ReadMetricQuery(t.Context(), metricquery.Scan{NodeID: nodeID, Metric: metric,
		Aggregation: "sum", From: from, To: from.Add(time.Hour), Interval: time.Minute, Buckets: 1})
	if err != nil {
		t.Fatalf("read metric %s: %v", metric, err)
	}
	total := 0.0
	for _, point := range values.Points {
		if point.Value != nil {
			total += *point.Value
		}
	}
	return total
}

func TestServiceFinalizesNodeDeletionAndErasesSeries(t *testing.T) {
	fixture := newErasureFixture(t)
	at := model.NowUTC().Add(-time.Minute)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 42, at); err != nil {
		t.Fatal(err)
	}
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	hidden, err := store.NewNodeStore(fixture.sqlite.DB).ListNodes(t.Context(), "")
	if err != nil || len(hidden) != 0 {
		t.Fatalf("tombstoned node still listed: %v %v", hidden, err)
	}
	if finalized, err := fixture.drain(t); err != nil || finalized.State != model.MetricErasureSucceeded {
		t.Fatalf("job state = %s error = %s", finalized.State, finalized.LastErrorCode)
	}
	if total := metricTotal(t, fixture.mts, fixture.nodeID, "cpu"); total != 0 {
		t.Fatalf("erased node metric total = %v", total)
	}
	var rows int
	if err := fixture.sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = ?", fixture.nodeID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("finalized node rows = %d", rows)
	}
	if job.Scope != model.MetricErasureDeleteNode {
		t.Fatalf("job scope = %s", job.Scope)
	}
}

func TestServiceFleetErasurePreservesProbeSeries(t *testing.T) {
	fixture := newErasureFixture(t)
	at := model.NowUTC().Add(-time.Minute)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 7, at); err != nil {
		t.Fatal(err)
	}
	if err := fixture.mts.WriteNetworkProbes(t.Context(),
		[]store.NetworkProbeSample{{TaskID: "task-1", NodeID: fixture.nodeID, TaskType: "icmp",
			FinishedAt: at, LatencyMS: 1.5, Success: true}}); err != nil {
		t.Fatal(err)
	}
	fixture.create(t, model.MetricErasureFleetTelemetry, "", model.MetricErasureFleetConfirmation)
	if finalized, err := fixture.drain(t); err != nil || finalized.State != model.MetricErasureSucceeded {
		t.Fatalf("job state = %s error = %s", finalized.State, finalized.LastErrorCode)
	}
	if total := metricTotal(t, fixture.mts, fixture.nodeID, "cpu"); total != 0 {
		t.Fatalf("fleet erasure left node metric total %v", total)
	}
	if !probePresent(t, fixture.mts, fixture.nodeID) {
		t.Fatal("fleet erasure removed probe history")
	}
}

func TestServiceFailsClosedOnUnregisteredMeasurement(t *testing.T) {
	fixture := newErasureFixture(t)
	at := model.NowUTC().Add(-time.Minute)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "rogue_series", 1, at); err != nil {
		t.Fatal(err)
	}
	fixture.create(t, model.MetricErasureAllMetricHistory, "", model.MetricErasureAllConfirmation)
	finalized, processErr := fixture.drain(t)
	if processErr == nil {
		t.Fatal("unregistered measurement did not fail the job")
	}
	if finalized.State != model.MetricErasureFailed ||
		finalized.LastErrorCode != model.MetricErasureErrorInventory {
		t.Fatalf("fail-closed job = %s %s", finalized.State, finalized.LastErrorCode)
	}
}

func probePresent(t *testing.T, mts *store.MTSStore, nodeID string) bool {
	t.Helper()
	latest, err := mts.QueryNetworkLatest(t.Context(), "task-1", nodeID)
	if err != nil {
		t.Fatalf("read network probe: %v", err)
	}
	return latest != nil
}
