package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/openmts/mts"
)

func TestMTSCompactionKeepsRepeatedFlushesHealthy(t *testing.T) {
	metrics := setupTestMTS(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, batch := range [][]TimePoint{
		{{Timestamp: base, Value: 10}, {Timestamp: base.Add(time.Second), Value: 20}},
		{{Timestamp: base.Add(time.Second), Value: 30}, {Timestamp: base.Add(2 * time.Second), Value: 40}},
	} {
		for _, point := range batch {
			if err := metrics.WriteMetric(t.Context(), "compact-node", "cpu", point.Value, point.Timestamp); err != nil {
				t.Fatal(err)
			}
		}
		if err := metrics.Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	assertCompactedHistory(t, metrics, base)
}

func TestMTSCompactionRepairsExistingOverlapOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics")
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seedUncompactedHistory(t, path, base)
	metrics, err := NewMTSStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := metrics.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	assertCompactedHistory(t, metrics, base)
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewMTSStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := reopened.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	assertCompactedHistory(t, reopened, base)
}

func seedUncompactedHistory(t *testing.T, path string, base time.Time) {
	t.Helper()
	options := mts.DefaultOptions(path)
	options.DefaultDatabase = "beat"
	engine, err := mts.Open(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := engine.Close(t.Context()); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	for index, values := range [][]float64{{10, 20}, {30, 40}} {
		points := make([]mts.Point, 0, len(values))
		for offset, value := range values {
			points = append(points, mts.Point{
				Measurement: "cpu", Tags: map[string]string{"node": "compact-node"},
				Fields:    map[string]mts.FieldValue{"value": mts.Float64Value(value)},
				Timestamp: base.Add(time.Duration(index+offset) * time.Second).Unix(), Precision: mts.PrecisionSecond,
			})
		}
		if err := engine.Write(t.Context(), points, mts.WriteOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := engine.Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if health := engine.HealthSnapshot(); health.Healthy {
		t.Fatal("overlapping uncompacted fixture unexpectedly healthy")
	}
}

func assertCompactedHistory(t *testing.T, metrics *MTSStore, base time.Time) {
	t.Helper()
	if healthy, reasons := metrics.Health(); !healthy {
		t.Fatalf("compaction did not restore health: %v", reasons)
	}
	rows, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, base.Add(-time.Second), base.Add(3*time.Second), "compact-node")
	if err != nil {
		t.Fatal(err)
	}
	want := []TimePoint{{Timestamp: base, Value: 10}, {Timestamp: base.Add(time.Second), Value: 30}, {Timestamp: base.Add(2 * time.Second), Value: 40}}
	if !reflect.DeepEqual(want, rows["cpu"]) {
		t.Fatalf("history after compaction: want %+v, got %+v", want, rows["cpu"])
	}
}
