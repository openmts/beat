package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// TestTrafficCalibrationRejectsInvalidAndConflictingWrites 覆盖校准追加的输入门禁与
// 「只能撤销当前生效事件」的冲突规则。
func TestTrafficCalibrationRejectsInvalidAndConflictingWrites(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	write := calibrationWrite(t, nodeID, time.Now().UTC(), 100, 200, calibrationTestUUID(1))
	first, err := ledger.AppendSet(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendRevoke(t.Context(), TrafficCalibrationRevokeWrite{}); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
		t.Fatalf("空撤销请求 = %v", err)
	}
	conflictRequest := calibrationTestUUID(2)
	conflict := TrafficCalibrationRevokeWrite{RequestID: conflictRequest, EventID: uuid.NewString(),
		NodeName: "node", Window: calibrationWindow(nodeID, write.EffectiveAt),
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner", RequestID: conflictRequest}, Now: write.Now}
	if _, err := ledger.AppendRevoke(t.Context(), conflict); !errors.Is(err, model.ErrTrafficCalibrationConflict) {
		t.Fatalf("撤销未知事件 = %v", err)
	}
	invalid := calibrationWrite(t, nodeID, write.EffectiveAt, 100, 200, "")
	if _, err := ledger.AppendSet(t.Context(), invalid); err == nil {
		t.Fatal("缺少请求号的校准写入被接受")
	}
	closed := NewTrafficCalibrationStore(closedSQLiteStore(t).DB)
	if _, err := closed.AppendSet(t.Context(), write); err == nil {
		t.Fatal("关闭的数据库接受了校准写入")
	}
	closedRequest := calibrationTestUUID(3)
	if _, err := closed.AppendRevoke(t.Context(), TrafficCalibrationRevokeWrite{RequestID: closedRequest,
		EventID: first.Event.ID, NodeName: "node", Window: calibrationWindow(nodeID, write.EffectiveAt),
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner", RequestID: closedRequest}, Now: write.Now}); err == nil {
		t.Fatal("关闭的数据库接受了撤销写入")
	}
}

// TestTrafficCalibrationFailsClosedWhenTheLedgerWriteFails 确认事件表写入失败时
// 整个事务失败关闭，不会留下半截校准。
func TestTrafficCalibrationFailsClosedWhenTheLedgerWriteFails(t *testing.T) {
	ledger, nodeID := calibrationLedgerFixture(t)
	if _, err := ledger.db.ExecContext(t.Context(), `CREATE TRIGGER cov_block_calibration BEFORE INSERT ON traffic_calibration_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	write := calibrationWrite(t, nodeID, time.Now().UTC(), 100, 200, calibrationTestUUID(9))
	if _, err := ledger.AppendSet(t.Context(), write); err == nil {
		t.Fatal("被阻断的校准事件写入了账本")
	}
	var count int
	if err := ledger.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM traffic_calibration_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("失败写入留下 %d 条校准事件", count)
	}
}
