package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestTrafficCalibrationRejectionWritesRedactedFailureAudit(t *testing.T) {
	f := newMetadataFixture(t)
	ledger := NewTrafficCalibrationStore(f.db)
	if err := ledger.RecordRejection(f.ctx, TrafficCalibrationRejection{
		NodeID: f.node.ID, NodeName: f.node.Name, Reason: "conflict"}); err != nil {
		t.Fatalf("record rejection: %v", err)
	}
	var action, outcome, resourceType, detail string
	err := f.db.QueryRow(`SELECT action,outcome,resource_type,detail_json FROM admin_audit_events
		ORDER BY rowid DESC LIMIT 1`).Scan(&action, &outcome, &resourceType, &detail)
	if err != nil {
		t.Fatal(err)
	}
	if action != "traffic_calibration.reject" || outcome != model.AuditOutcomeFailure ||
		resourceType != "traffic_calibration" {
		t.Fatalf("rejection audit = %s %s %s", action, outcome, resourceType)
	}
	if !strings.Contains(detail, "reason:conflict") {
		t.Fatalf("rejection detail = %s", detail)
	}
	var events int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM traffic_calibration_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("rejection appended %d ledger rows", events)
	}
}

func TestTrafficCalibrationRejectionRequiresLiveAuthority(t *testing.T) {
	f := newMetadataFixture(t)
	ledger := NewTrafficCalibrationStore(f.db)
	// 没有审计主体时（内部调用方）沿用既有约定：不写审计也不报错。
	if err := ledger.RecordRejection(t.Context(), TrafficCalibrationRejection{
		NodeID: f.node.ID, NodeName: f.node.Name, Reason: "invalid"}); err != nil {
		t.Fatalf("actorless rejection error = %v", err)
	}
	if _, err := f.db.Exec("UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordRejection(f.ctx, TrafficCalibrationRejection{
		NodeID: f.node.ID, NodeName: f.node.Name, Reason: "invalid"}); !errors.Is(err, ErrMetadataForbidden) {
		t.Fatalf("revoked rejection error = %v", err)
	}
	var audits int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM admin_audit_events").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("unauthorized rejection wrote %d audits", audits)
	}
	if _, err := f.db.Exec("DROP TABLE admin_audit_events"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordRejection(f.ctx, TrafficCalibrationRejection{Reason: "invalid"}); err == nil {
		t.Fatal("failing audit was reported as success")
	}
}
