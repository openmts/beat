package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/openmts/mts"
	_ "modernc.org/sqlite"
)

func TestSQLiteSnapshotCreatesIndependentDatabase(t *testing.T) {
	store := setupTestDB(t)
	destination := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := store.Snapshot(t.Context(), destination); err != nil {
		t.Fatalf("create SQLite snapshot: %v", err)
	}
	database, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatalf("open SQLite snapshot: %v", err)
	}
	defer func() { _ = database.Close() }()
	var groups int
	if err := database.QueryRow("SELECT COUNT(*) FROM groups").Scan(&groups); err != nil || groups != 1 {
		t.Fatalf("snapshot groups = %d, %v", groups, err)
	}
	if err := store.Snapshot(t.Context(), destination); err == nil {
		t.Fatal("existing snapshot destination accepted")
	}
}

func TestMTSExportImportRowsRoundTrip(t *testing.T) {
	source := setupTestMTS(t)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Nanosecond)
	if err := source.WriteMetric(t.Context(), "node", "cpu", 25, timestamp); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if err := source.WriteNetworkProbes(t.Context(), []NetworkProbeSample{{TaskID: "task", NodeID: "node",
		TaskType: "tcp", FinishedAt: timestamp, LatencyMS: 3, Success: true, ErrorCode: "none"}}); err != nil {
		t.Fatalf("write network probe: %v", err)
	}
	rows := []mts.Row{}
	count, err := source.ExportRows(t.Context(), timestamp.Add(time.Second), func(row mts.Row) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil || count != 2 || len(rows) != 2 {
		t.Fatalf("export rows = %d/%d, %v", count, len(rows), err)
	}
	if _, err := source.ExportRows(t.Context(), timestamp, nil); err == nil {
		t.Fatal("nil MTS row consumer accepted")
	}
	destination, err := NewMTSStore(filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatalf("new restored MTS: %v", err)
	}
	defer func() { _ = destination.Close() }()
	points := make([]mts.Point, 0, len(rows))
	for _, row := range rows {
		points = append(points, mts.Point{Measurement: row.Measurement, Tags: row.Tags,
			Timestamp: row.Timestamp, Precision: mts.PrecisionNanosecond, Fields: row.Fields})
	}
	if err := destination.ImportPoints(t.Context(), points); err != nil {
		t.Fatalf("import points: %v", err)
	}
	if err := destination.ImportPoints(context.Background(), nil); err != nil {
		t.Fatalf("import empty points: %v", err)
	}
	metrics, err := destination.QueryMetrics(t.Context(), []string{"cpu"},
		timestamp.Add(-time.Second), timestamp.Add(time.Second), "node")
	if err != nil || len(metrics["cpu"]) != 1 || metrics["cpu"][0].Value != 25 {
		t.Fatalf("restored metrics = %#v, %v", metrics, err)
	}
}

func TestMTSExportRowsConsumerFailureAborts(t *testing.T) {
	source := setupTestMTS(t)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Nanosecond)
	if err := source.WriteMetric(t.Context(), "node", "cpu", 25, timestamp); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	sentinel := errors.New("consumer aborted")
	count, err := source.ExportRows(t.Context(), timestamp.Add(time.Second), func(mts.Row) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("export error = %v, want consumer error", err)
	}
	if count != 0 {
		t.Fatalf("export count = %d, want 0", count)
	}
}

func TestManagedMeasurementsMatchExportSchemas(t *testing.T) {
	managed := managedMeasurements()
	schemas := exportMeasurementSchemas()
	exported := make([]string, 0, len(schemas))
	seen := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		if _, duplicate := seen[schema.name]; duplicate {
			t.Fatalf("duplicate MTS export schema %q", schema.name)
		}
		seen[schema.name] = struct{}{}
		exported = append(exported, schema.name)
	}
	if !slices.Equal(managed, exported) {
		t.Fatalf("managed MTS measurements = %v, exported measurements = %v", managed, exported)
	}
}
