package backup

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const trafficCalibrationEventsSchema = `CREATE TABLE traffic_calibration_events(
	id TEXT, node_id TEXT, node_name TEXT, kind TEXT, effective_at DATETIME,
	target_sent INTEGER, target_received INTEGER, reason TEXT, actor_id TEXT,
	actor_name TEXT, session_id TEXT, request_id TEXT, reference_event_id TEXT,
	created_at DATETIME)`

const trafficCalibrationPreamble = "CREATE TABLE schema_migrations(version INTEGER)"

func TestValidateTrafficCalibrationLedgerReadFaults(t *testing.T) {
	t.Run("ledger table unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, trafficCalibrationPreamble,
			"INSERT INTO schema_migrations(version) VALUES(30)")
		handled, err := validateTrafficCalibrationLedger(t.Context(), path)
		if !handled || err == nil {
			t.Fatalf("missing calibration table = %v/%v", handled, err)
		}
	})
	t.Run("node identifiers unreadable", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, trafficCalibrationPreamble,
			"INSERT INTO schema_migrations(version) VALUES(30)", trafficCalibrationEventsSchema)
		handled, err := validateTrafficCalibrationLedger(t.Context(), path)
		if !handled || err == nil {
			t.Fatalf("missing node table = %v/%v", handled, err)
		}
	})
	t.Run("node identifier is null", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, trafficCalibrationPreamble,
			"INSERT INTO schema_migrations(version) VALUES(30)", trafficCalibrationEventsSchema,
			"CREATE TABLE nodes(id TEXT)", "INSERT INTO nodes(id) VALUES(NULL)")
		handled, err := validateTrafficCalibrationLedger(t.Context(), path)
		if !handled || err == nil {
			t.Fatalf("null node identifier = %v/%v", handled, err)
		}
	})
	t.Run("older snapshot is skipped", func(t *testing.T) {
		path := erasureLedgerSnapshot(t, trafficCalibrationPreamble,
			"INSERT INTO schema_migrations(version) VALUES(3)")
		handled, err := validateTrafficCalibrationLedger(t.Context(), path)
		if handled || err != nil {
			t.Fatalf("older snapshot = %v/%v", handled, err)
		}
	})
}

func TestValidateArchivedRevokeRejectsBrokenReferenceChains(t *testing.T) {
	node := "node-1"
	target := "set-1"
	created := time.Unix(1700000000, 0).UTC()
	revoke := model.TrafficCalibrationEvent{ID: "revoke-1", NodeID: &node,
		ReferenceEventID: &target, CreatedAt: created}
	if err := validateArchivedRevoke(revoke, target, map[string]model.TrafficCalibrationEvent{}); err == nil {
		t.Fatal("revoke without an appended set was accepted")
	}
	later := model.TrafficCalibrationEvent{ID: target, NodeID: &node, CreatedAt: created.Add(time.Second)}
	if err := validateArchivedRevoke(revoke, target,
		map[string]model.TrafficCalibrationEvent{target: later}); err == nil {
		t.Fatal("revoke of a later set was accepted")
	}
	other := "node-2"
	crossNode := model.TrafficCalibrationEvent{ID: target, NodeID: &other, CreatedAt: created}
	if err := validateArchivedRevoke(revoke, target,
		map[string]model.TrafficCalibrationEvent{target: crossNode}); err == nil {
		t.Fatal("revoke of another node's set was accepted")
	}
	same := model.TrafficCalibrationEvent{ID: target, NodeID: &node, CreatedAt: created}
	if err := validateArchivedRevoke(revoke, target,
		map[string]model.TrafficCalibrationEvent{target: same}); err != nil {
		t.Fatalf("valid revoke error = %v", err)
	}
}
