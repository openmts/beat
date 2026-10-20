package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func calibrationLedgerFixture(t *testing.T) (*TrafficCalibrationStore, string) {
	t.Helper()
	sqlite := setupTestDB(t)
	t.Cleanup(func() { _ = sqlite.Close() })
	nodeID := "11111111-1111-1111-1111-111111111111"
	if _, err := sqlite.DB.ExecContext(t.Context(),
		`INSERT INTO nodes (id, name, host, port, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID, "node", "192.0.2.1", 22, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	return NewTrafficCalibrationStore(sqlite.DB), nodeID
}

func calibrationWrite(t *testing.T, nodeID string, effective time.Time, sent, received int64, requestID string) TrafficCalibrationSetWrite {
	t.Helper()
	return TrafficCalibrationSetWrite{
		Request: model.TrafficCalibrationRequest{RequestID: requestID, TargetSent: sent,
			TargetReceived: received, Reason: "provider reconciliation"},
		NodeID: nodeID, NodeName: "node", EffectiveAt: effective,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner", RequestID: requestID},
		Now:   effective.Add(time.Minute),
	}
}

func calibrationWindow(nodeID string, start time.Time) TrafficCalibrationWindow {
	return TrafficCalibrationWindow{NodeID: nodeID, Start: start, End: start.AddDate(0, 1, 0)}
}

func TestTrafficCalibrationAppendAndReplay(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	effective := start.Add(12 * time.Hour)
	requestID := calibrationTestUUID(1)

	change, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, effective, 1000, 2000, requestID))
	if err != nil || change.Replayed {
		t.Fatalf("append set = %#v %v", change, err)
	}
	if change.Event.TargetSent == nil || *change.Event.TargetSent != 1000 {
		t.Fatalf("target not persisted: %#v", change.Event)
	}
	latest, err := ledger.LatestState(t.Context(), calibrationWindow(nodeID, start))
	if err != nil || latest == nil || latest.ID != change.Event.ID || !latest.Calibrated() {
		t.Fatalf("latest state = %#v %v", latest, err)
	}

	replay, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, effective, 1000, 2000, requestID))
	if err != nil || !replay.Replayed || replay.Event.ID != change.Event.ID {
		t.Fatalf("identical replay = %#v %v", replay, err)
	}
	_, err = ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, effective, 9999, 2000, requestID))
	if !errors.Is(err, model.ErrTrafficCalibrationConflict) {
		t.Fatalf("conflicting reuse error = %v", err)
	}
	if _, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, effective, 1, 1, "not-a-uuid")); err == nil {
		t.Fatal("invalid request id should have failed")
	}
}

func TestTrafficCalibrationRevokeAndReplace(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	window := calibrationWindow(nodeID, start)
	first, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, start.Add(time.Hour), 10, 20, calibrationTestUUID(1)))
	if err != nil {
		t.Fatalf("append first set: %v", err)
	}
	revoke, err := ledger.AppendRevoke(t.Context(), TrafficCalibrationRevokeWrite{
		RequestID: calibrationTestUUID(2), NodeName: "node", Window: window, EventID: first.Event.ID,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: start.Add(2 * time.Hour)})
	if err != nil || revoke.Revoked == nil || revoke.Revoked.Kind != model.TrafficCalibrationRevoke {
		t.Fatalf("revoke = %#v %v", revoke, err)
	}
	latest, err := ledger.LatestState(t.Context(), window)
	if err != nil || latest == nil || latest.Calibrated() {
		t.Fatalf("post-revoke state = %#v %v", latest, err)
	}
	_, err = ledger.AppendRevoke(t.Context(), TrafficCalibrationRevokeWrite{
		RequestID: calibrationTestUUID(3), NodeName: "node", Window: window, EventID: first.Event.ID,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: start.Add(3 * time.Hour)})
	if !errors.Is(err, model.ErrTrafficCalibrationConflict) {
		t.Fatalf("revoking a stale set error = %v", err)
	}

	second, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, start.Add(4*time.Hour), 30, 40, calibrationTestUUID(4)))
	if err != nil {
		t.Fatalf("append second set: %v", err)
	}
	replace, err := ledger.AppendReplace(t.Context(), TrafficCalibrationReplaceWrite{
		Request: model.TrafficCalibrationRequest{RequestID: calibrationTestUUID(5), TargetSent: 55,
			TargetReceived: 66, Reason: "correction"},
		NodeName: "node", EffectiveAt: start.Add(5 * time.Hour), Window: window, EventID: second.Event.ID,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: start.Add(6 * time.Hour)})
	if err != nil || replace.Revoked == nil {
		t.Fatalf("replace = %#v %v", replace, err)
	}
	latest, err = ledger.LatestState(t.Context(), window)
	if err != nil || latest == nil || latest.ID != replace.Event.ID || !latest.Calibrated() {
		t.Fatalf("post-replace state = %#v %v", latest, err)
	}
}

func TestTrafficCalibrationHistoryPaginationAndEventLookup(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	for index := range 3 {
		_, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID,
			start.Add(time.Duration(index+1)*time.Hour), int64(index), int64(index), calibrationTestUUID(index+1)))
		if err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	first, err := ledger.History(t.Context(), TrafficCalibrationHistoryRequest{NodeID: nodeID, Limit: 2})
	if err != nil || len(first.Events) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v %v", first, err)
	}
	second, err := ledger.History(t.Context(), TrafficCalibrationHistoryRequest{NodeID: nodeID, Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Events) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %#v %v", second, err)
	}
	if second.Events[0].ID == first.Events[0].ID || second.Events[0].ID == first.Events[1].ID {
		t.Fatal("pagination repeated an event")
	}
	if _, err := ledger.History(t.Context(), TrafficCalibrationHistoryRequest{NodeID: nodeID, Cursor: "not-base64"}); err == nil {
		t.Fatal("invalid cursor should have failed")
	}
	if _, err := ledger.History(t.Context(), TrafficCalibrationHistoryRequest{NodeID: nodeID, Limit: MaximumTrafficCalibrationRows + 1}); err == nil {
		t.Fatal("oversized page should have failed")
	}
	if _, err := ledger.History(t.Context(), TrafficCalibrationHistoryRequest{}); err == nil {
		t.Fatal("missing node should have failed")
	}
	found, err := ledger.Event(t.Context(), nodeID, first.Events[0].ID)
	if err != nil || found == nil || found.ID != first.Events[0].ID {
		t.Fatalf("event lookup = %#v %v", found, err)
	}
	other, err := ledger.Event(t.Context(), "22222222-2222-2222-2222-222222222222", first.Events[0].ID)
	if err != nil || other != nil {
		t.Fatalf("cross-node lookup = %#v %v", other, err)
	}
	if _, err := ledger.Event(t.Context(), nodeID, "bad"); err == nil {
		t.Fatal("invalid event id should have failed")
	}
	if _, err := ledger.LatestState(t.Context(), TrafficCalibrationWindow{NodeID: nodeID}); err == nil {
		t.Fatal("invalid window should have failed")
	}
}

func TestTrafficCalibrationEvidenceRedactsTargets(t *testing.T) {
	effective := time.Now().UTC()
	event := calibrationEvidenceEvent(t, effective)
	evidence := trafficCalibrationEvidence(event)
	if evidence.Action != "traffic_calibration.set" || evidence.Type != "traffic_calibration" {
		t.Fatalf("evidence = %#v", evidence)
	}
	for _, field := range evidence.Fields {
		if field == "target_sent" || field == "target_received" {
			continue
		}
	}
	joined := ""
	for _, field := range evidence.Fields {
		joined += field + ","
	}
	if joined == "" || len(evidence.Fields) == 0 {
		t.Fatal("evidence fields missing")
	}
	for _, secret := range []string{"1000", "2000", "provider reconciliation"} {
		if containsField(evidence.Fields, secret) {
			t.Fatalf("evidence leaked %q: %#v", secret, evidence.Fields)
		}
	}
	if evidence.Name != event.NodeName {
		t.Fatalf("evidence name = %q", evidence.Name)
	}
}

func calibrationEvidenceEvent(t *testing.T, effective time.Time) model.TrafficCalibrationEvent {
	t.Helper()
	node := "node-1"
	sent, received := int64(1000), int64(2000)
	instant := effective.UTC()
	return model.TrafficCalibrationEvent{ID: calibrationTestUUID(9), NodeID: &node, NodeName: "node",
		Kind: model.TrafficCalibrationSet, EffectiveAt: &instant, TargetSent: &sent, TargetReceived: &received,
		Reason: "provider reconciliation", CreatedAt: effective, Actor: model.TrafficCalibrationActor{
			ID: "user-1", Name: "owner", RequestID: calibrationTestUUID(10)}}
}

func containsField(fields []string, value string) bool {
	for _, field := range fields {
		if field == value {
			return true
		}
	}
	return false
}

func calibrationTestUUID(index int) string {
	const alphabet = "0123456789abcdef"
	digits := make([]byte, 0, 32)
	for range 32 {
		digits = append(digits, alphabet[index%len(alphabet)])
		index /= len(alphabet)
	}
	return string(digits[0:8]) + "-" + string(digits[8:12]) + "-" + string(digits[12:16]) + "-" +
		string(digits[16:20]) + "-" + string(digits[20:32])
}
