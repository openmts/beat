package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricquery"
)

const maximumRawMetricPoints = 100_000

const maximumRawMetricFields = 64

type rawMetricRange struct {
	metrics    []string
	nodeID     string
	start, end time.Time
}

type rawMetricSelection struct {
	metric, nodeID string
	start, end     time.Time
	limit          int
}

func (s *MTSStore) QueryMetrics(ctx context.Context, metrics []string, start, end time.Time, nodeID string) (map[string][]TimePoint, error) {
	return s.queryRawMetricRange(ctx, rawMetricRange{metrics: metrics, nodeID: nodeID, start: start, end: end})
}

func (s *MTSStore) queryRawMetricRange(ctx context.Context, request rawMetricRange) (map[string][]TimePoint, error) {
	if err := validateRawMetricRange(request); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, metricquery.QueryTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	result := make(map[string][]TimePoint, len(request.metrics))
	remaining := maximumRawMetricPoints
	for _, metric := range request.metrics {
		selection := rawMetricSelection{metric: metric, nodeID: request.nodeID, start: request.start, end: request.end, limit: remaining}
		points, err := s.queryRawMetric(ctx, selection)
		if err != nil {
			return nil, err
		}
		remaining -= len(points)
		result[metric] = points
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	return result, nil
}

func validateRawMetricRange(request rawMetricRange) error {
	if len(request.metrics) > maximumRawMetricFields || !request.end.After(request.start) || request.end.Sub(request.start) > metricquery.MaximumRange {
		return metricquery.ErrInvalid
	}
	if request.start.Before(time.Unix(0, 0)) || request.end.After(time.Unix(0, math.MaxInt64)) {
		return metricquery.ErrInvalid
	}
	for _, metric := range request.metrics {
		if !metriccatalog.ValidKey(metric) {
			return metricquery.ErrInvalid
		}
	}
	return nil
}

func (s *MTSStore) queryRawMetric(ctx context.Context, selection rawMetricSelection) ([]TimePoint, error) {
	query, err := mts.NewQuery().From("beat", "", selection.metric).Select("value").
		TimeRange(selection.start.UnixNano(), selection.end.UnixNano()).Where(mts.TagEq("node", selection.nodeID)).
		Precision(mts.PrecisionNanosecond).OrderByTimeAsc().Limit(selection.limit + 1).Build()
	if err != nil {
		return nil, fmt.Errorf("build raw metric query: %w", err)
	}
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: metricquery.MaximumScannedSamples}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, metricQueryReadError(err)
	}
	if len(rows) > selection.limit {
		return nil, metricquery.ErrCapacity
	}
	points := make([]TimePoint, 0, len(rows))
	for _, row := range rows {
		value, ok := row.Fields["value"]
		if !ok {
			continue
		}
		if value.Type != mts.FieldFloat64 || math.IsNaN(value.Float64) || math.IsInf(value.Float64, 0) {
			return nil, metricquery.ErrArithmetic
		}
		points = append(points, TimePoint{Timestamp: time.Unix(0, row.Timestamp).UTC(), Value: value.Float64})
	}
	return points, nil
}
