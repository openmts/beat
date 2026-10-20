package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const covCalibrationNodeID = "77777777-7777-4777-8777-777777777777"

// calibrationMetadataFixture 在带已授权元数据主体的库上再建一个节点，
// 用来验证校准账本在审计写入失败时整体回滚。
func calibrationMetadataFixture(t *testing.T) (*TrafficCalibrationStore, string, context.Context) {
	t.Helper()
	fixture := newMetadataFixture(t)
	now := model.NowUTC()
	if _, err := fixture.db.ExecContext(t.Context(),
		`INSERT INTO nodes (id, name, alias, group_id, host, port, ssh_public_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		covCalibrationNodeID, "calibration", "Calibration", fixture.group.ID, "192.0.2.7", 22, "", now, now); err != nil {
		t.Fatal(err)
	}
	return NewTrafficCalibrationStore(fixture.db), covCalibrationNodeID, fixture.ctx
}

func calibrationRevokeWrite(nodeID, requestID, eventID string, window TrafficCalibrationWindow,
	now time.Time) TrafficCalibrationRevokeWrite {
	return TrafficCalibrationRevokeWrite{RequestID: requestID, NodeName: "node", Window: window,
		EventID: eventID, Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: now}
}

func calibrationReplaceWrite(nodeID, requestID, eventID string, window TrafficCalibrationWindow,
	effective, now time.Time) TrafficCalibrationReplaceWrite {
	return TrafficCalibrationReplaceWrite{
		Request: model.TrafficCalibrationRequest{RequestID: requestID, TargetSent: 30, TargetReceived: 40,
			Reason: "provider reconciliation"},
		NodeName: "node", EffectiveAt: effective, Window: window, EventID: eventID,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: now}
}

// TestTrafficCalibrationAppendRejectsInvalidWrites 覆盖 set 组装的参数门禁，
// 以及对替换请求的请求号校验。
func TestTrafficCalibrationAppendRejectsInvalidWrites(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	effective := start.Add(12 * time.Hour)
	for name, mutate := range map[string]func(*TrafficCalibrationSetWrite){
		"missing node": func(write *TrafficCalibrationSetWrite) { write.NodeID = "" },
		"missing effective time": func(write *TrafficCalibrationSetWrite) {
			write.EffectiveAt = time.Time{}
		},
		"missing node name": func(write *TrafficCalibrationSetWrite) { write.NodeName = "" },
	} {
		t.Run(name, func(t *testing.T) {
			write := calibrationWrite(t, nodeID, effective, 1, 2, calibrationTestUUID(11))
			mutate(&write)
			if _, err := ledger.AppendSet(t.Context(), write); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
				t.Fatalf("invalid set = %v", err)
			}
		})
	}
	replace := calibrationReplaceWrite(nodeID, "bad-request", calibrationTestUUID(14),
		calibrationWindow(nodeID, start), effective, effective.Add(time.Minute))
	if _, err := ledger.AppendReplace(t.Context(), replace); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
		t.Fatalf("invalid replacement = %v", err)
	}
}

// TestTrafficCalibrationAppendFailsClosed 覆盖 set 追加的授权、重放读取、
// 审计与提交四类失败面。
func TestTrafficCalibrationAppendFailsClosed(t *testing.T) {
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	effective := start.Add(12 * time.Hour)
	t.Run("unauthorized metadata actor", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{
			UserID: "ghost", SessionID: "ghost-session"}})
		write := calibrationWrite(t, nodeID, effective, 1, 2, calibrationTestUUID(21))
		if _, err := ledger.AppendSet(ctx, write); err == nil {
			t.Fatal("unauthorized actor appended a set")
		}
	})
	t.Run("replay lookup", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		if _, err := ledger.db.ExecContext(t.Context(),
			"ALTER TABLE traffic_calibration_events RENAME TO cov_calibration_backup"); err != nil {
			t.Fatal(err)
		}
		write := calibrationWrite(t, nodeID, effective, 1, 2, calibrationTestUUID(22))
		if _, err := ledger.AppendSet(t.Context(), write); err == nil {
			t.Fatal("unreadable replay ledger was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		ledger, nodeID, ctx := calibrationMetadataFixture(t)
		installCustomMetricFault(t, ledger.db, `CREATE TRIGGER cov_calibration_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		write := calibrationWrite(t, nodeID, effective, 1, 2, calibrationTestUUID(23))
		if _, err := ledger.AppendSet(ctx, write); err == nil {
			t.Fatal("blocked audit was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		installCustomMetricCommitFault(t, ledger.db, "INSERT ON traffic_calibration_events")
		write := calibrationWrite(t, nodeID, effective, 1, 2, calibrationTestUUID(24))
		if _, err := ledger.AppendSet(t.Context(), write); err == nil {
			t.Fatal("deferred commit failure was accepted")
		}
	})
}

func seedCalibrationSet(t *testing.T, ledger *TrafficCalibrationStore, nodeID string,
	effective time.Time, requestID string) model.TrafficCalibrationEvent {
	t.Helper()
	change, err := ledger.AppendSet(t.Context(), calibrationWrite(t, nodeID, effective, 10, 20, requestID))
	if err != nil {
		t.Fatal(err)
	}
	return change.Event
}

// TestTrafficCalibrationRevokeAndReplaceFailsClosed 覆盖 revoke/replace 的窗口校验、
// 归属校验、重放读取与授权失败。
func TestTrafficCalibrationRevokeAndReplaceFailsClosed(t *testing.T) {
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	t.Run("invalid window", func(t *testing.T) {
		ledger, _ := calibrationLedgerFixture(t)
		window := TrafficCalibrationWindow{Start: start, End: start.Add(time.Hour)}
		write := calibrationRevokeWrite("", calibrationTestUUID(31), calibrationTestUUID(32), window, start)
		if _, err := ledger.AppendRevoke(t.Context(), write); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
			t.Fatalf("invalid window = %v", err)
		}
	})
	t.Run("invalid attribution", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		window := calibrationWindow(nodeID, start)
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(33), calibrationTestUUID(34), window, start)
		write.NodeName = strings.Repeat("n", 300)
		if _, err := ledger.AppendRevoke(t.Context(), write); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
			t.Fatalf("invalid revoke attribution = %v", err)
		}
	})
	t.Run("unauthorized metadata actor", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		window := calibrationWindow(nodeID, start)
		ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{
			UserID: "ghost", SessionID: "ghost-session"}})
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(35), calibrationTestUUID(36), window, start)
		if _, err := ledger.AppendRevoke(ctx, write); err == nil {
			t.Fatal("unauthorized actor revoked a calibration")
		}
	})
	t.Run("replay lookup", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		if _, err := ledger.db.ExecContext(t.Context(),
			"ALTER TABLE traffic_calibration_events RENAME TO cov_calibration_backup"); err != nil {
			t.Fatal(err)
		}
		window := calibrationWindow(nodeID, start)
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(37), calibrationTestUUID(38), window, start)
		if _, err := ledger.AppendRevoke(t.Context(), write); err == nil {
			t.Fatal("unreadable revoke ledger was accepted")
		}
	})
}

// TestTrafficCalibrationRevokeReplay 覆盖 revoke 的幂等重放返回原事件与已落库撤销。
func TestTrafficCalibrationRevokeReplay(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	window := calibrationWindow(nodeID, start)
	active := seedCalibrationSet(t, ledger, nodeID, start.Add(time.Hour), calibrationTestUUID(49))
	write := calibrationRevokeWrite(nodeID, calibrationTestUUID(50), active.ID, window, start.Add(2*time.Hour))
	if _, err := ledger.AppendRevoke(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	replay, err := ledger.AppendRevoke(t.Context(), write)
	if err != nil || !replay.Replayed || replay.Event.ID == "" || replay.Revoked == nil {
		t.Fatalf("revoke replay = %#v %v", replay, err)
	}
}

// TestTrafficCalibrationLedgerWriteFaults 覆盖两步插入的写入阻断、提交失败与审计阻断。
func TestTrafficCalibrationLedgerWriteFaults(t *testing.T) {
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	t.Run("revoke insert blocked", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		window := calibrationWindow(nodeID, start)
		active := seedCalibrationSet(t, ledger, nodeID, start.Add(time.Hour), calibrationTestUUID(41))
		installCustomMetricFault(t, ledger.db, `CREATE TRIGGER cov_revoke_block
			BEFORE INSERT ON traffic_calibration_events WHEN NEW.kind='revoke'
			BEGIN SELECT RAISE(ABORT,'revoke blocked'); END`)
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(42), active.ID, window, start.Add(2*time.Hour))
		if _, err := ledger.AppendRevoke(t.Context(), write); err == nil {
			t.Fatal("blocked revoke insert was accepted")
		}
	})
	t.Run("replacement insert blocked", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		window := calibrationWindow(nodeID, start)
		active := seedCalibrationSet(t, ledger, nodeID, start.Add(time.Hour), calibrationTestUUID(43))
		installCustomMetricFault(t, ledger.db, `CREATE TRIGGER cov_replace_block
			BEFORE INSERT ON traffic_calibration_events WHEN NEW.kind='set'
			BEGIN SELECT RAISE(ABORT,'replacement blocked'); END`)
		write := calibrationReplaceWrite(nodeID, calibrationTestUUID(44), active.ID, window,
			start.Add(2*time.Hour), start.Add(3*time.Hour))
		if _, err := ledger.AppendReplace(t.Context(), write); err == nil {
			t.Fatal("blocked replacement insert was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		ledger, nodeID := calibrationLedgerFixture(t)
		window := calibrationWindow(nodeID, start)
		active := seedCalibrationSet(t, ledger, nodeID, start.Add(time.Hour), calibrationTestUUID(45))
		installCustomMetricCommitFault(t, ledger.db, "INSERT ON traffic_calibration_events")
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(46), active.ID, window, start.Add(2*time.Hour))
		if _, err := ledger.AppendRevoke(t.Context(), write); err == nil {
			t.Fatal("deferred commit failure was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		ledger, nodeID, ctx := calibrationMetadataFixture(t)
		window := calibrationWindow(nodeID, start)
		active := seedCalibrationSet(t, ledger, nodeID, start.Add(time.Hour), calibrationTestUUID(47))
		installCustomMetricFault(t, ledger.db, `CREATE TRIGGER cov_calibration_revoke_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		write := calibrationRevokeWrite(nodeID, calibrationTestUUID(48), active.ID, window, start.Add(2*time.Hour))
		if _, err := ledger.AppendRevoke(ctx, write); err == nil {
			t.Fatal("blocked revoke audit was accepted")
		}
	})
}

// TestTrafficCalibrationCursorEncodingEdges 确认空事件号编码为空游标。
func TestTrafficCalibrationCursorEncodingEdges(t *testing.T) {
	if got := EncodeTrafficCalibrationCursor(""); got != "" {
		t.Fatalf("empty cursor encoding = %q", got)
	}
}
