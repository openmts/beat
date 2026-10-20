package backup

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const metricErasureArchiveSchema = `
CREATE TABLE schema_migrations(version INTEGER);
CREATE TABLE nodes(id TEXT);
CREATE TABLE network_tasks(id TEXT);
CREATE TABLE metric_erasure_jobs(
	id TEXT, scope TEXT, target_type TEXT, target_id TEXT, target_name TEXT, state TEXT,
	cutoff_unix_nano INTEGER, measurement_cursor TEXT, attempt_count INTEGER,
	last_error_code TEXT, request_id TEXT, requested_at DATETIME, completed_at DATETIME);`

type metricErasureArchiveRow struct {
	id, scope, targetType, targetID, name, state string
	attempts                                     int
	requestID                                    string
	cutoff                                       *int64
	completed                                    *time.Time
}

func metricErasureArchiveFixture(t *testing.T, version int64, rows []metricErasureArchiveRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(metricErasureArchiveSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO schema_migrations(version) VALUES(?)", version); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO nodes(id) VALUES('22222222-2222-4222-8222-222222222222')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO network_tasks(id) VALUES('33333333-3333-4333-8333-333333333333')"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := database.Exec(`INSERT INTO metric_erasure_jobs VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.scope, row.targetType, row.targetID, row.name, row.state, row.cutoff, "",
			row.attempts, "", row.requestID, time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC),
			row.completed); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func archivedErasureRow(id, scope, targetType, targetID, state string) metricErasureArchiveRow {
	return metricErasureArchiveRow{id: id, scope: scope, targetType: targetType, targetID: targetID,
		name: "target", state: state, requestID: id}
}

func TestMetricErasureArchiveSkipsPreV22Snapshots(t *testing.T) {
	row := archivedErasureRow("11111111-1111-4111-8111-111111111111", model.MetricErasureFleetTelemetry,
		model.MetricErasureTargetNone, "", model.MetricErasureQueued)
	path := metricErasureArchiveFixture(t, 21, []metricErasureArchiveRow{row})
	handled, err := validateMetricErasureLedger(t.Context(), path)
	if handled || err != nil {
		t.Fatalf("pre-v22 ledger = %v %v, want legacy handling", handled, err)
	}
}

func TestMetricErasureArchiveAcceptsAConsistentLedger(t *testing.T) {
	cutoff := time.Date(2026, time.October, 15, 8, 30, 0, 0, time.UTC).UnixNano()
	completed := time.Date(2026, time.October, 15, 8, 31, 0, 0, time.UTC)
	active := archivedErasureRow("11111111-1111-4111-8111-111111111111", model.MetricErasureDeleteNode,
		model.MetricErasureTargetNode, "22222222-2222-4222-8222-222222222222", model.MetricErasureDeleting)
	active.cutoff = &cutoff
	finishedNode := archivedErasureRow("22222222-2222-4222-8222-222222222222", model.MetricErasureDeleteNode,
		model.MetricErasureTargetNode, "99999999-9999-4999-8999-999999999999", model.MetricErasureSucceeded)
	finishedNode.cutoff, finishedNode.completed = &cutoff, &completed
	fleet := archivedErasureRow("33333333-3333-4333-8333-333333333333", model.MetricErasureFleetTelemetry,
		model.MetricErasureTargetNone, "", model.MetricErasureQueued)

	path := metricErasureArchiveFixture(t, 22, []metricErasureArchiveRow{active, finishedNode, fleet})
	handled, err := validateMetricErasureLedger(t.Context(), path)
	if !handled || err != nil {
		t.Fatalf("consistent ledger = %v %v, want acceptance", handled, err)
	}
}

func TestMetricErasureArchiveRejectsCorruptedLedgers(t *testing.T) {
	cutoff := time.Date(2026, time.October, 15, 8, 30, 0, 0, time.UTC).UnixNano()
	completed := time.Date(2026, time.October, 15, 8, 31, 0, 0, time.UTC)
	activeNode := archivedErasureRow("11111111-1111-4111-8111-111111111111", model.MetricErasureNodeHistory,
		model.MetricErasureTargetNode, "22222222-2222-4222-8222-222222222222", model.MetricErasureQueued)

	mismatched := activeNode
	mismatched.id, mismatched.requestID = "44444444-4444-4444-8444-444444444444", "44444444-4444-4444-8444-444444444444"
	mismatched.targetType = model.MetricErasureTargetNetworkTask

	orphan := activeNode
	orphan.id, orphan.requestID = "55555555-5555-4555-8555-555555555555", "55555555-5555-4555-8555-555555555555"
	orphan.targetID = "88888888-8888-4888-8888-888888888888"

	terminalWithoutCompletion := archivedErasureRow("66666666-6666-4666-8666-666666666666",
		model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "", model.MetricErasureSucceeded)
	terminalWithoutCompletion.cutoff = &cutoff

	failedWithoutCode := archivedErasureRow("77777777-7777-4777-8777-777777777777",
		model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "", model.MetricErasureFailed)
	failedWithoutCode.completed = &completed

	missingCutoff := archivedErasureRow("88888888-8888-4888-8888-888888888888",
		model.MetricErasureAllMetricHistory, model.MetricErasureTargetNone, "", model.MetricErasureCompacting)

	badAttempts := activeNode
	badAttempts.id, badAttempts.requestID = "99999999-9999-4999-8999-999999999999", "99999999-9999-4999-8999-999999999999"
	badAttempts.attempts = store.MaximumMetricErasureAttempts + 1

	duplicateTarget := activeNode
	duplicateTarget.id, duplicateTarget.requestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	duplicateFleet := archivedErasureRow("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "", model.MetricErasureQueued)
	duplicateFleetSecond := archivedErasureRow("cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "", model.MetricErasureClaimed)

	for name, rows := range map[string][]metricErasureArchiveRow{
		"mismatched target type": {mismatched},
		"orphan node":            {orphan},
		"terminal without time":  {terminalWithoutCompletion},
		"failure without code":   {failedWithoutCode},
		"missing frozen cutoff":  {missingCutoff},
		"out-of-range attempts":  {badAttempts},
		"two active same target": {activeNode, duplicateTarget},
		"two active fleet scope": {duplicateFleet, duplicateFleetSecond},
		"invalid scope":          {metricErasureArchiveRow{id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", scope: "everything", requestID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}},
	} {
		t.Run(name, func(t *testing.T) {
			path := metricErasureArchiveFixture(t, 22, rows)
			if handled, err := validateMetricErasureLedger(t.Context(), path); !handled || err == nil {
				t.Fatalf("corrupted ledger = %v %v, want rejection", handled, err)
			}
		})
	}
}
