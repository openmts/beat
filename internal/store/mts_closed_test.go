package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// setupClosedTestMTS 返回一个写入样本后已关闭的存储。
func setupClosedTestMTS(t *testing.T) *MTSStore {
	t.Helper()
	s := setupTestMTS(t)
	ctx := context.Background()
	now := time.Now().UTC()
	err := s.WriteMetric(ctx, "node-closed", "cpu", 42.0, now)
	if err != nil {
		t.Fatalf("seed metric before close: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flush before close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return s
}

func assertClosedError(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s after Close: expected error, got nil", name)
	}
	if !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("%s after Close: expected ErrMTSStoreClosed, got %v", name, err)
	}
}

func TestMTSOperationsAfterClose(t *testing.T) {
	s := setupClosedTestMTS(t)
	ctx := context.Background()

	_, err := s.QueryMetrics(ctx, []string{"cpu"}, time.Now().Add(-time.Hour), time.Now(), "node-closed")
	assertClosedError(t, "QueryMetrics", err)

	_, err = s.QueryLatest(ctx, "node-closed")
	assertClosedError(t, "QueryLatest", err)

	assertClosedError(t, "WriteMetric", s.WriteMetric(ctx, "node-closed", "cpu", 1.0, time.Now()))
	assertClosedError(t, "WriteNodeMetrics", s.WriteNodeMetrics(ctx, NodeMetricSample{
		NodeID:    "node-closed",
		Metrics:   model.NodeMetrics{CPU: 1},
		Timestamp: time.Now(),
	}))

	assertClosedError(t, "Flush", s.Flush(ctx))
	assertClosedError(t, "CleanupBefore", s.CleanupBefore(ctx, time.Now().Add(time.Hour)))
	_, err = s.DiskUsage()
	assertClosedError(t, "DiskUsage", err)

	assertClosedError(t, "WriteNetworkProbes", s.WriteNetworkProbes(ctx, []NetworkProbeSample{{
		TaskID: "task-1", NodeID: "node-closed", TaskType: "icmp",
		FinishedAt: time.Now(), LatencyMS: 1.0, Success: true,
	}}))

	latest, err := s.QueryNetworkLatest(ctx, "task-1", "node-closed")
	assertClosedError(t, "QueryNetworkLatest", err)
	if latest != nil {
		t.Fatalf("QueryNetworkLatest after Close: expected nil result, got %+v", latest)
	}

	points, err := s.QueryNetworkHistory(ctx, "task-1", "node-closed",
		time.Now().Add(-time.Hour), time.Now())
	assertClosedError(t, "QueryNetworkHistory", err)
	if points != nil {
		t.Fatalf("QueryNetworkHistory after Close: expected nil result, got %+v", points)
	}

	assertClosedError(t, "DeleteNetworkTask", s.DeleteNetworkTask(ctx, "task-1"))

	rows, err := s.ExportRows(ctx, time.Now().Add(time.Hour), func(mts.Row) error { return nil })
	assertClosedError(t, "ExportRows", err)
	if rows != 0 {
		t.Fatalf("ExportRows after Close: expected 0 rows, got %d", rows)
	}

	assertClosedError(t, "ImportPoints", s.ImportPoints(ctx, []mts.Point{{
		Measurement: "cpu",
		Tags:        map[string]string{"node": "node-closed"},
		Fields:      map[string]mts.FieldValue{"value": mts.Float64Value(1)},
	}}))
}

func TestMTSCloseIdempotent(t *testing.T) {
	s := setupTestMTS(t)
	for range 3 {
		if err := s.Close(); err != nil {
			t.Fatalf("repeated close must stay nil, got %v", err)
		}
	}
}

func TestMTSHealthAfterClose(t *testing.T) {
	s := setupClosedTestMTS(t)

	healthy, reasons := s.Health()
	if healthy {
		t.Fatal("Health after Close: expected unhealthy")
	}
	if len(reasons) == 0 {
		t.Fatal("Health after Close: expected at least one reason")
	}
}

func TestMTSConcurrentCloseAndQuery(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	done := make(chan struct{})

	go func() {
		defer close(done)
		for range 50 {
			_, _ = s.QueryLatest(ctx, "node-race")
		}
	}()

	if err := s.Close(); err != nil {
		t.Fatalf("close during queries: %v", err)
	}
	<-done
}

func TestExportRowsConsumerErrorPropagates(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	now := time.Now().UTC()
	consumerErr := errors.New("consumer failure")

	if err := s.WriteMetric(ctx, "n1", "cpu", 1.0, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	count, err := s.ExportRows(ctx, now.Add(time.Minute), func(mts.Row) error {
		return consumerErr
	})
	if !errors.Is(err, consumerErr) {
		t.Fatalf("expected consumer error, got %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 consumed rows, got %d", count)
	}
}

func TestImportPointsEmptyIsNoop(t *testing.T) {
	s := setupTestMTS(t)
	if err := s.ImportPoints(context.Background(), nil); err != nil {
		t.Fatalf("empty import must be noop, got %v", err)
	}
}
