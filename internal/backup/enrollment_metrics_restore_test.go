package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/store"
)

func joinedNodeMetricLines(t *testing.T, count int) ([]string, time.Time) {
	t.Helper()
	start := time.Now().UTC().Add(-time.Hour)
	lines := make([]string, 0, count)
	for index := range count {
		record := metricRecord{Measurement: "cpu", Tags: map[string]string{"node": "restored-agent"},
			TimestampNS: start.Add(time.Duration(index) * time.Second).UnixNano(), Fields: map[string]mts.FieldValue{"value": mts.Float64Value(float64(index))}}
		content, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(content))
	}
	return lines, start
}

func TestRestoreMetricBatchReplaysAllRowsAndRejectsClosedStorage(t *testing.T) {
	const sampleCount = 501
	lines, start := joinedNodeMetricLines(t, sampleCount)
	path := filepath.Join(t.TempDir(), "joined-node.jsonl.gz")
	writeMetricLines(t, path, lines)
	destination, err := newTestMTS(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayMetrics(t.Context(), path, destination); err != nil {
		t.Fatal(err)
	}
	result, err := destination.QueryMetrics(t.Context(), []string{"cpu"}, start.Add(-time.Second), start.Add(sampleCount*time.Second), "restored-agent")
	if err != nil || len(result["cpu"]) != sampleCount {
		t.Fatalf("batch restore lost samples: count=%d error=%v", len(result["cpu"]), err)
	}
	if result["cpu"][0].Value != 0 || result["cpu"][sampleCount-1].Value != sampleCount-1 {
		t.Fatal("batch boundaries changed metric values")
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	if err := replayMetrics(t.Context(), path, destination); !errors.Is(err, store.ErrMTSStoreClosed) {
		t.Fatalf("closed restore destination: %v", err)
	}
}

func TestRestoreRejectsMetricGzipChecksumFailure(t *testing.T) {
	lines, _ := joinedNodeMetricLines(t, 1)
	path := filepath.Join(t.TempDir(), "damaged.jsonl.gz")
	writeMetricLines(t, path, lines)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const gzipFooterBytes = 8
	content[len(content)-gzipFooterBytes] ^= 1
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateMetrics(path, time.Now().UnixNano()); err == nil {
		t.Fatal("damaged metric payload passed validation")
	}
	destination, err := newTestMTS(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayMetrics(t.Context(), path, destination); err == nil {
		t.Fatal("damaged metric payload replayed successfully")
	}
	latest, err := destination.QueryLatest(t.Context(), "restored-agent")
	if err != nil || len(latest) != 0 {
		t.Fatalf("damaged final batch was written: %v", err)
	}
	if err := importMetrics(t.Context(), path, filepath.Join(t.TempDir(), "restore-mts")); err == nil {
		t.Fatal("damaged restore was accepted")
	}
}
