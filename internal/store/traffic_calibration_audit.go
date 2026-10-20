package store

import (
	"context"

	"github.com/beat/backend/internal/model"
)

// trafficCalibrationEvidence 只登记字段名，绝不把目标字节值或原因正文写进审计详情。
func trafficCalibrationEvidence(event model.TrafficCalibrationEvent) metadataEvidence {
	fields := []string{"kind"}
	if event.Calibrated() {
		fields = append(fields, "effective_at", "target_sent", "target_received")
	}
	if event.ReferenceEventID != nil {
		fields = append(fields, "reference_event_id")
	}
	fields = append(fields, "reason", "actor")
	return metadataEvidence{Action: "traffic_calibration." + event.Kind, Type: "traffic_calibration",
		ID: event.ID, Name: event.NodeName, Fields: fields}
}

// TrafficCalibrationRejection 描述一次被拒绝的校准写入。Reason 只能是调用方
// 定义的分类常量，绝不包含目标字节值、原因正文或任何请求内容。
type TrafficCalibrationRejection struct {
	NodeID   string
	NodeName string
	Reason   string
}

// RecordRejection 追加一条失败的校准审计；它不修改账本，也不需要账本锁。
func (store *TrafficCalibrationStore) RecordRejection(
	ctx context.Context,
	rejection TrafficCalibrationRejection,
) error {
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	if err := mutation.reject(trafficCalibrationRejectionEvidence(rejection)); err != nil {
		return err
	}
	return tx.Commit()
}

func trafficCalibrationRejectionEvidence(rejection TrafficCalibrationRejection) metadataEvidence {
	return metadataEvidence{Action: "traffic_calibration.reject", Type: "traffic_calibration",
		ID: rejection.NodeID, Name: rejection.NodeName, Fields: []string{"reason:" + rejection.Reason}}
}
