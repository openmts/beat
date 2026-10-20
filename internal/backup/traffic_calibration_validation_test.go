package backup

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

const calibrationArchiveSchema = `
CREATE TABLE schema_migrations(version INTEGER);
CREATE TABLE nodes(id TEXT);
CREATE TABLE traffic_calibration_events(
	id TEXT, node_id TEXT, node_name TEXT, kind TEXT, effective_at DATETIME,
	target_sent INTEGER, target_received INTEGER, reason TEXT, actor_id TEXT,
	actor_name TEXT, session_id TEXT, request_id TEXT, reference_event_id TEXT,
	created_at DATETIME);`

type calibrationArchiveRow struct {
	id, node, name, kind, reason string
	effective                    *time.Time
	sent, received               *int64
	reference                    *string
	createdAt                    time.Time
}

func calibrationArchiveFixture(t *testing.T, version int64, rows []calibrationArchiveRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(calibrationArchiveSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO schema_migrations(version) VALUES(?)", version); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO nodes(id) VALUES('node-1')"); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		_, err := database.Exec(`INSERT INTO traffic_calibration_events VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.node, row.name, row.kind, row.effective, row.sent, row.received, row.reason,
			"user-1", "owner", "session", calibrationLedgerRequestID(index), row.reference, row.createdAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func calibrationLedgerRequestID(index int) string {
	return "22222222-2222-2222-2222-22222222222" + string(rune('0'+index))
}

func calibrationLedgerEvent(id, node string) calibrationArchiveRow {
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	sent, received := int64(1000), int64(2000)
	return calibrationArchiveRow{id: id, node: node, name: "node", kind: "set", reason: "opening balance",
		effective: &effective, sent: &sent, received: &received, createdAt: effective.Add(time.Minute)}
}

func TestTrafficCalibrationArchiveSkipsPreV21Snapshots(t *testing.T) {
	path := calibrationArchiveFixture(t, 20, []calibrationArchiveRow{calibrationLedgerEvent("11111111-1111-1111-1111-111111111111", "node-1")})
	handled, err := validateTrafficCalibrationLedger(t.Context(), path)
	if handled || err != nil {
		t.Fatalf("pre-v21 ledger = %v %v, want legacy handling", handled, err)
	}
}

func TestTrafficCalibrationArchiveAcceptsAConsistentLedger(t *testing.T) {
	first := calibrationLedgerEvent("11111111-1111-1111-1111-111111111111", "node-1")
	revoke := calibrationArchiveRow{id: "11111111-1111-1111-1111-111111111112", node: "node-1", name: "node",
		kind: "revoke", reason: "revoked by administrator", reference: &first.id, createdAt: first.createdAt.Add(time.Minute)}
	path := calibrationArchiveFixture(t, 21, []calibrationArchiveRow{first, revoke})
	handled, err := validateTrafficCalibrationLedger(t.Context(), path)
	if !handled || err != nil {
		t.Fatalf("consistent ledger = %v %v", handled, err)
	}
}

func TestTrafficCalibrationArchiveRejectsBrokenLedgers(t *testing.T) {
	first := calibrationLedgerEvent("11111111-1111-1111-1111-111111111111", "node-1")
	second := calibrationLedgerEvent("11111111-1111-1111-1111-111111111112", "node-1")
	second.createdAt = first.createdAt.Add(time.Minute)
	subSecond := calibrationLedgerEvent("11111111-1111-1111-1111-111111111113", "node-1")
	effective := first.effective.Add(500 * time.Millisecond)
	subSecond.effective = &effective
	orphan := calibrationLedgerEvent("11111111-1111-1111-1111-111111111114", "node-2")
	staleRevoke := calibrationArchiveRow{id: "11111111-1111-1111-1111-111111111115", node: "node-1", name: "node",
		kind: "revoke", reason: "revoked by administrator", reference: &first.id,
		createdAt: second.createdAt.Add(time.Minute)}
	longReason := calibrationLedgerEvent("11111111-1111-1111-1111-111111111116", "node-1")
	longReason.reason = string(make([]byte, 501))
	for name, rows := range map[string][]calibrationArchiveRow{
		"sub-second instant": {subSecond},
		"orphan node":        {orphan},
		"stale revoke":       {first, second, staleRevoke},
		"oversized reason":   {longReason},
	} {
		t.Run(name, func(t *testing.T) {
			path := calibrationArchiveFixture(t, 21, rows)
			if handled, err := validateTrafficCalibrationLedger(t.Context(), path); !handled || err == nil {
				t.Fatalf("broken ledger = %v %v, want failure", handled, err)
			}
		})
	}
}
