package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// calibrationRawRow 允许把可选列写成 NULL，用于覆盖归档快照里的历史形状。
type calibrationRawRow struct {
	id            string
	node          *string
	name          string
	kind          string
	effective     *time.Time
	sent          *int64
	received      *int64
	reason        string
	reference     *string
	createdAt     *time.Time
	omitCreatedAt bool
}

func calibrationRawFixture(t *testing.T, version *int64, rows []calibrationRawRow) string {
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
	if version != nil {
		if _, err := database.Exec("INSERT INTO schema_migrations(version) VALUES(?)", *version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec("INSERT INTO nodes(id) VALUES('node-1')"); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		var createdAt any
		if !row.omitCreatedAt {
			createdAt = row.createdAt
		}
		if _, err := database.Exec(`INSERT INTO traffic_calibration_events VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.node, row.name, row.kind, row.effective, row.sent, row.received, row.reason,
			"user-1", "owner", "session", calibrationLedgerRequestID(index), row.reference, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func stringPointer(value string) *string { return &value }

func timePointer(value time.Time) *time.Time { return &value }

func TestTrafficCalibrationArchiveSkipsSnapshotsWithoutLedger(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "fresh.sqlite")
	database, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE nodes(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if handled, err := validateTrafficCalibrationLedger(t.Context(), empty); handled || err != nil {
		t.Fatalf("snapshot without ledger = %v %v", handled, err)
	}
	blank := calibrationRawFixture(t, nil, nil)
	if handled, err := validateTrafficCalibrationLedger(t.Context(), blank); handled || err != nil {
		t.Fatalf("snapshot without schema history = %v %v", handled, err)
	}
}

func TestTrafficCalibrationArchiveRejectsUnreadableSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTrafficCalibrationLedger(t.Context(), path); err == nil {
		t.Fatal("corrupt snapshot was accepted")
	}
}

func TestTrafficCalibrationArchiveRejectsMalformedRows(t *testing.T) {
	version := int64(21)
	path := calibrationRawFixture(t, &version, []calibrationRawRow{{
		id: "11111111-1111-1111-1111-111111111111", name: "node", kind: "set",
		effective: timePointer(time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)),
		sent:      func() *int64 { value := int64(1); return &value }(),
		received:  func() *int64 { value := int64(2); return &value }(),
		reason:    "opening balance", omitCreatedAt: true,
	}})
	if handled, err := validateTrafficCalibrationLedger(t.Context(), path); !handled || err == nil {
		t.Fatalf("malformed row = %v %v", handled, err)
	}
}

func TestTrafficCalibrationArchiveValidatesRevokeReferences(t *testing.T) {
	version := int64(21)
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	sent, received := int64(1000), int64(2000)
	created := effective.Add(time.Minute)
	otherNode := "node-2"
	set := calibrationRawRow{id: "11111111-1111-1111-1111-111111111111", node: stringPointer("node-1"),
		name: "node", kind: "set", effective: &effective, sent: &sent, received: &received,
		reason: "opening balance", createdAt: &created}
	cases := []struct {
		name string
		rows []calibrationRawRow
	}{
		{name: "revoke without active set", rows: []calibrationRawRow{{
			id: "11111111-1111-1111-1111-111111111112", node: stringPointer("node-1"), name: "node",
			kind: "revoke", reason: "revoked by administrator", reference: stringPointer(set.id), createdAt: &created}}},
		{name: "revoke of an unknown event", rows: []calibrationRawRow{set, {
			id: "11111111-1111-1111-1111-111111111113", node: stringPointer("node-1"), name: "node",
			kind: "revoke", reason: "revoked by administrator",
			reference: stringPointer("11111111-1111-1111-1111-111111111199"), createdAt: &created}}},
		{name: "revoke across nodes", rows: []calibrationRawRow{set, {
			id: "11111111-1111-1111-1111-111111111114", node: &otherNode, name: "other",
			kind: "revoke", reason: "revoked by administrator", reference: stringPointer(set.id), createdAt: &created}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := calibrationRawFixture(t, &version, test.rows)
			if handled, err := validateTrafficCalibrationLedger(t.Context(), path); !handled || err == nil {
				t.Fatalf("broken revoke = %v %v", handled, err)
			}
		})
	}
}

func TestTrafficCalibrationArchiveHandlesDeletedNodes(t *testing.T) {
	version := int64(21)
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	sent, received := int64(1000), int64(2000)
	created := effective.Add(time.Minute)
	set := calibrationRawRow{id: "11111111-1111-1111-1111-111111111111", name: "retired-node",
		kind: "set", effective: &effective, sent: &sent, received: &received,
		reason: "opening balance", createdAt: &created}
	revoke := calibrationRawRow{id: "11111111-1111-1111-1111-111111111112", name: "retired-node",
		kind: "revoke", reason: "revoked by administrator", reference: stringPointer(set.id), createdAt: &created}
	path := calibrationRawFixture(t, &version, []calibrationRawRow{set, revoke})
	if handled, err := validateTrafficCalibrationLedger(t.Context(), path); !handled || err != nil {
		t.Fatalf("deleted-node ledger = %v %v", handled, err)
	}
	path = calibrationRawFixture(t, &version, []calibrationRawRow{set, {
		id: revoke.id, node: stringPointer("node-1"), name: "retired-node", kind: "revoke",
		reason: "revoked by administrator", reference: stringPointer(set.id), createdAt: &created}})
	if handled, err := validateTrafficCalibrationLedger(t.Context(), path); !handled || err == nil {
		t.Fatalf("recreated-node ledger = %v %v", handled, err)
	}
}
