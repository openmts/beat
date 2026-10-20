package store

import (
	"context"
	"time"

	"github.com/beat/backend/internal/model"
)

// TrafficAccountingResolver 把原始 MTS 周期流量与追加式校准账本合成当前周期的账面结算值。
//
// 它只读取两者，不写入任何 MTS 行：原始历史、图表与运营报表永远保持未调整。没有
// 生效校准事件时结果与原始聚合完全一致。
type TrafficAccountingResolver struct {
	mts         *MTSStore
	calibration *TrafficCalibrationStore
}

func NewTrafficAccountingResolver(mts *MTSStore, calibration *TrafficCalibrationStore) *TrafficAccountingResolver {
	return &TrafficAccountingResolver{mts: mts, calibration: calibration}
}

// CycleTotals 返回节点当前计费周期的账面流量与结算元数据。
func (r *TrafficAccountingResolver) CycleTotals(
	ctx context.Context,
	node model.Node,
	now time.Time,
) (model.TrafficTotals, model.TrafficAccounting, error) {
	if r == nil || r.mts == nil {
		return model.TrafficTotals{}, model.TrafficAccounting{Mode: model.TrafficAccountingRaw}, nil
	}
	start, next := model.BillingPeriod(now, node.TrafficResetDay)
	raw, err := r.mts.QueryTrafficUsage(ctx, node.ID, start, next)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	rawAccounting := model.TrafficAccounting{Mode: model.TrafficAccountingRaw}
	if r.calibration == nil {
		return raw, rawAccounting, nil
	}
	window := TrafficCalibrationWindow{NodeID: node.ID, Start: start, End: next}
	event, err := r.calibration.LatestState(ctx, window)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	if event == nil || !event.Calibrated() {
		return raw, rawAccounting, nil
	}
	// v22 成功擦除流量历史后，边界之前的校准目标不再适用于后续结算。
	cutoff, erased, err := r.calibration.LatestErasureCutoff(ctx, node.ID)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	if erased && !event.Anchor().After(cutoff) {
		return raw, rawAccounting, nil
	}
	after, err := r.mts.QueryTrafficUsageAfter(ctx, node.ID, event.EffectiveAt.UTC(), next)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	effective := event.EffectiveAt.UTC()
	calibratedStart := start
	totals := model.TrafficTotals{
		Sent:         float64(*event.TargetSent) + after.Sent,
		Received:     float64(*event.TargetReceived) + after.Received,
		TrackedSince: &calibratedStart,
	}
	return totals, model.TrafficAccounting{Mode: model.TrafficAccountingCalibrated, CalibratedAt: &effective}, nil
}

// PreviewTotals 返回一次候选校准生效秒之后的原始周期流量，供 UI 预览使用。
func (r *TrafficAccountingResolver) PreviewTotals(
	ctx context.Context,
	node model.Node,
	effective time.Time,
	now time.Time,
) (model.TrafficTotals, model.TrafficAccounting, error) {
	if r == nil || r.mts == nil {
		return model.TrafficTotals{}, model.TrafficAccounting{Mode: model.TrafficAccountingRaw}, nil
	}
	start, next := model.BillingPeriod(now, node.TrafficResetDay)
	raw, err := r.mts.QueryTrafficUsage(ctx, node.ID, start, next)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	after, err := r.mts.QueryTrafficUsageAfter(ctx, node.ID, effective.UTC(), next)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	at := effective.UTC()
	return model.TrafficTotals{Sent: raw.Sent - after.Sent, Received: raw.Received - after.Received, TrackedSince: &at},
		model.TrafficAccounting{Mode: model.TrafficAccountingCalibrated, CalibratedAt: &at}, nil
}
