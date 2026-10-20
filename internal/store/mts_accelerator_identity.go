package store

import (
	"context"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func (s *MTSStore) checkAcceleratorIdentity(ctx context.Context, nodeID string, report model.AcceleratorReport) error {
	query, err := mts.NewQuery().From("beat", "", model.AcceleratorSampleMeasurement).Select(acceleratorSampleFieldNames()...).
		Where(mts.TagEq("node", nodeID), mts.FieldEq("sample_id", mts.StringValue(report.SampleID))).
		TimeRange(report.CapturedAt.Add(-2*model.MaximumAcceleratorInterval).UnixNano(), report.CapturedAt.Add(model.MaximumAcceleratorInterval).UnixNano()).Precision(mts.PrecisionNanosecond).OrderByTimeDesc().Limit(1).Build()
	if err != nil {
		return err
	}
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: maximumAgentReceiptScan}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return err
	}
	if len(rows) > 0 && rows[0].Timestamp != report.CapturedAt.UnixNano() {
		return ErrAgentSampleConflict
	}
	latest, err := s.readAcceleratorSample(ctx, nodeID, time.Now().Add(model.MaximumAgentClockSkew).UnixNano())
	if err != nil {
		return err
	}
	if latest != nil && report.CapturedAt.Before(latest.Snapshot.CapturedAt) {
		return ErrAgentSampleOrder
	}
	return nil
}
