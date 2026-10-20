package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/openmts/mts"
)

type latestScalarQuery struct {
	nodeID, metric string
	end            time.Time
}

func (s *MTSStore) queryLatestScalar(ctx context.Context, selection latestScalarQuery) (float64, bool, error) {
	point, err := s.queryLatestScalarPoint(ctx, selection)
	if err != nil || point == nil {
		return 0, false, err
	}
	return point.Value, true, nil
}

func (s *MTSStore) queryLatestScalarPoint(ctx context.Context, selection latestScalarQuery) (*TimePoint, error) {
	plan := latestScanPlan(time.Unix(0, 0), selection.end, selection.end)
	rows, err := scanLatestRows(ctx, plan, func(ctx context.Context, from, to time.Time) ([]mts.Row, error) {
		return s.queryLatestScalarRows(ctx, latestScalarRange{nodeID: selection.nodeID, metric: selection.metric, from: from, to: to})
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	value, ok := rows[0].Fields["value"]
	if !ok || value.Type != mts.FieldFloat64 || math.IsNaN(value.Float64) || math.IsInf(value.Float64, 0) {
		return nil, errors.New("invalid latest metric scalar")
	}
	return &TimePoint{Timestamp: time.Unix(0, rows[0].Timestamp).UTC(), Value: value.Float64}, nil
}

type latestScalarRange struct {
	nodeID, metric string
	from, to       time.Time
}

// queryLatestScalarRows 在给定范围内按时间倒序取一条样本。
func (s *MTSStore) queryLatestScalarRows(ctx context.Context, span latestScalarRange) ([]mts.Row, error) {
	query, err := mts.NewQuery().From("beat", "", span.metric).Select("value").TimeRange(span.from.UnixNano(), span.to.UnixNano()).
		Where(mts.TagEq("node", span.nodeID)).Precision(mts.PrecisionNanosecond).OrderByTimeDesc().Limit(1).Build()
	if err != nil {
		return nil, err
	}
	query.Budget.MaxSamples = maximumAgentReceiptScan
	query.Budget.MaxShards = metricQueryMaximumShards
	query.Budget.MaxParts = metricQueryMaximumParts
	return s.engine.QueryRows(ctx, query)
}
