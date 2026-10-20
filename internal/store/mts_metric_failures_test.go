package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

type faultyMetricIterator struct {
	row  mts.Row
	err  error
	next bool
}

func (iterator *faultyMetricIterator) Next() bool {
	value := iterator.next
	iterator.next = false
	return value
}

func (iterator *faultyMetricIterator) Row() mts.Row { return iterator.row }

func (iterator *faultyMetricIterator) Err() error { return iterator.err }

func (*faultyMetricIterator) Close() error { return nil }

func TestMTSMetricIteratorRejectsCorruptionCancellationAndBudgetErrors(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	plan := metricquery.Scan{From: start, To: start.Add(time.Minute), Interval: time.Minute, Buckets: 1, Aggregation: "sum"}
	for _, row := range []mts.Row{{Fields: map[string]mts.FieldValue{}}, {Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}}, {Fields: map[string]mts.FieldValue{"value": mts.Float64Value(math.Inf(1))}}} {
		if err := consumeMetricQuery(t.Context(), &faultyMetricIterator{row: row, next: true}, metricquery.NewBuckets(plan)); err == nil {
			t.Fatal("invalid stored scalar became a valid query")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := consumeMetricQuery(ctx, &faultyMetricIterator{next: true}, metricquery.NewBuckets(plan)); !errors.Is(err, context.Canceled) {
		t.Fatal("iterator ignored caller cancellation")
	}
	failure := errors.New("iterator failed")
	if err := consumeMetricQuery(t.Context(), &faultyMetricIterator{err: failure}, metricquery.NewBuckets(plan)); !errors.Is(err, failure) {
		t.Fatal("iterator failure became empty success")
	}
	if err := metricQueryReadError(mts.ErrReadBudgetExceeded); !errors.Is(err, metricquery.ErrCapacity) {
		t.Fatal("MTS scan budget was not surfaced")
	}
}

func TestMTSCatalogAttachmentRejectsUnknownOrIncompatibleStorage(t *testing.T) {
	for _, scenario := range []string{"unknown", "invalid custom", "wrong type"} {
		t.Run(scenario, func(t *testing.T) {
			sqlite := setupTestDB(t)
			metrics := metricQueryMTS(t)
			name := "unknown"
			if scenario == "invalid custom" {
				name = "custom_bad/name"
			}
			if scenario == "wrong type" {
				name = "cpu"
				if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: name, Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}, Timestamp: time.Now().UnixNano()}}); err != nil {
					t.Fatal(err)
				}
			} else if err := metrics.WriteMetric(t.Context(), "node", name, 1, time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err == nil {
				t.Fatal("unsupported stored measurement admitted")
			}
		})
	}
	metrics := metricQueryMTS(t)
	if err := metrics.AttachMetricCatalog(t.Context(), nil); err == nil {
		t.Fatal("missing catalog accepted")
	}
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if err := metrics.AttachMetricCatalog(t.Context(), catalog); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed inventory attached")
	}
}

func TestMTSMetricRetentionRejectsInvalidAndClosedOperations(t *testing.T) {
	metrics := metricQueryMTS(t)
	for _, job := range []model.MetricRetentionJob{{Key: "bad name", RetentionDays: 1}, {Key: "cpu", RetentionDays: 0}, {Key: "cpu", RetentionDays: metriccatalog.MaximumRetentionDays + 1}} {
		if err := metrics.ApplyMetricRetention(t.Context(), job); err == nil {
			t.Fatal("invalid retention job accepted")
		}
	}
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ApplyMetricRetention(t.Context(), model.MetricRetentionJob{Key: "cpu", RetentionDays: 1, StartedAt: model.NowUTC()}); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed retention returned success")
	}
}

func TestMTSRawHistoryRejectsUnsafeRangeAndNeverSilentlyTruncates(t *testing.T) {
	metrics := metricQueryMTS(t)
	now := time.Now().UTC()
	for _, request := range []rawMetricRange{{metrics: []string{"cpu"}, start: now, end: now}, {metrics: []string{"cpu"}, start: now.Add(-2 * metricquery.MaximumRange), end: now}, {metrics: []string{"bad name"}, start: now.Add(-time.Hour), end: now}, {metrics: make([]string, maximumRawMetricFields+1), start: now.Add(-time.Hour), end: now}} {
		if _, err := metrics.queryRawMetricRange(t.Context(), request); !errors.Is(err, metricquery.ErrInvalid) {
			t.Fatal("unbounded raw query accepted")
		}
	}
	if err := metrics.WriteMetric(t.Context(), "node", "cpu", 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.queryRawMetric(t.Context(), rawMetricSelection{metric: "cpu", nodeID: "node", start: now.Add(-time.Second), end: now.Add(time.Second), limit: 0}); !errors.Is(err, metricquery.ErrCapacity) {
		t.Fatal("raw row limit silently truncated data")
	}
}

func TestMTSQueryHelpersPropagateClosedEngineAndInvalidPlans(t *testing.T) {
	metrics := metricQueryMTS(t)
	from := model.NowUTC()
	plan := metricquery.Scan{NodeID: "node", Metric: "cpu", From: from, To: from.Add(time.Minute), Aggregation: "avg", Buckets: 1, Interval: time.Minute}
	invalid := plan
	invalid.Buckets = 0
	if _, err := metrics.ReadMetricQuery(t.Context(), invalid); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatal("invalid bucket plan reached MTS")
	}
	if err := metrics.engine.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.ReadMetricQuery(t.Context(), plan); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed engine returned empty result")
	}
	if _, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, from, plan.To, "node"); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("raw query hid closed engine")
	}
	if _, err := metrics.QueryLatest(t.Context(), "node"); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("latest query hid closed engine")
	}
	if healthy, _ := metrics.Health(); healthy {
		t.Fatal("health hid closed engine")
	}
	sqlite := setupTestDB(t)
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err == nil {
		t.Fatal("unavailable engine inventory attached")
	}
	if err := metrics.ApplyMetricRetention(t.Context(), model.MetricRetentionJob{Key: "cpu", RetentionDays: 1, StartedAt: from}); err == nil {
		t.Fatal("failed retention flush ignored")
	}
}
