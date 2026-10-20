package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

const (
	maximumPublicInteractionBatch  = 64
	maximumPublicInteractionSeries = 64
)

// ErrPublicInteractionSampleRejected 表示匿名交互批次被拒绝；调用方一律以 204 应答。
var ErrPublicInteractionSampleRejected = errors.New("public interaction samples were rejected")

// PublicInteractionTotal 是一个聚合计数序列当前的总量；标签只来自固定枚举。
type PublicInteractionTotal struct {
	Metric  string
	Event   string
	Surface string
	Reason  string
	Total   float64
}

// WritePublicInteractions 把已接受的事件按固定标签聚合成一个计数点；数值绝不进入 SQLite。
// 同一批次内相同标签的事件合并计数，避免同一时间戳的点互相覆盖。
func (s *MTSStore) WritePublicInteractions(ctx context.Context, at time.Time, interactions []model.PublicInteraction) error {
	if at.IsZero() || len(interactions) == 0 || len(interactions) > maximumPublicInteractionBatch {
		return ErrPublicInteractionSampleRejected
	}
	counts := make(map[model.PublicInteraction]float64, len(interactions))
	for _, interaction := range interactions {
		if err := interaction.Validate(); err != nil {
			return ErrPublicInteractionSampleRejected
		}
		counts[interaction]++
	}
	points := make([]mts.Point, 0, len(counts))
	for interaction, count := range counts {
		points = append(points, mts.Point{Measurement: model.PublicInteractionTotalMetric,
			Tags: map[string]string{
				model.PublicInteractionTagEvent:   interaction.Event,
				model.PublicInteractionTagSurface: interaction.Surface,
			},
			Fields:    map[string]mts.FieldValue{model.PublicInteractionSampleField: mts.Float64Value(count)},
			Timestamp: at.UnixNano(), Precision: mts.PrecisionNanosecond})
	}
	if err := s.ImportPoints(ctx, points); err != nil {
		return fmt.Errorf("write public interactions: %w", err)
	}
	return nil
}

// WritePublicInteractionRejections 只累计固定原因枚举的被拒次数，并按原因合并同一批次。
func (s *MTSStore) WritePublicInteractionRejections(ctx context.Context, at time.Time, reasons []string) error {
	if at.IsZero() || len(reasons) == 0 || len(reasons) > maximumPublicInteractionBatch {
		return ErrPublicInteractionSampleRejected
	}
	counts := make(map[string]float64, len(reasons))
	for _, reason := range reasons {
		if !model.ValidPublicRejectionReason(reason) {
			return ErrPublicInteractionSampleRejected
		}
		counts[reason]++
	}
	points := make([]mts.Point, 0, len(counts))
	for reason, count := range counts {
		points = append(points, mts.Point{Measurement: model.PublicInteractionRejectedMetric,
			Tags:      map[string]string{model.PublicInteractionTagReason: reason},
			Fields:    map[string]mts.FieldValue{model.PublicInteractionSampleField: mts.Float64Value(count)},
			Timestamp: at.UnixNano(), Precision: mts.PrecisionNanosecond})
	}
	if err := s.ImportPoints(ctx, points); err != nil {
		return fmt.Errorf("write public interaction rejections: %w", err)
	}
	return nil
}

// ReadPublicInteractionTotals 返回请求区间内每个固定标签序列的总量，供管理端汇总。
func (s *MTSStore) ReadPublicInteractionTotals(ctx context.Context, from, to time.Time) ([]PublicInteractionTotal, error) {
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, metricquery.ErrInvalid
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
	totals := make([]PublicInteractionTotal, 0, maximumPublicInteractionSeries)
	for _, key := range model.PublicInteractionMetrics() {
		columns, err := s.queryPublicInteractionTotals(ctx, key, from, to)
		if err != nil {
			return nil, err
		}
		for _, column := range columns {
			total, err := publicInteractionTotalOf(key, column)
			if err != nil {
				return nil, err
			}
			totals = append(totals, total)
		}
	}
	return totals, ctx.Err()
}

func publicInteractionTotalOf(key string, column mts.ColumnSeries) (PublicInteractionTotal, error) {
	if len(column.Values) != 1 || column.Values[0].Type != mts.FieldFloat64 ||
		math.IsNaN(column.Values[0].Float64) || math.IsInf(column.Values[0].Float64, 0) {
		return PublicInteractionTotal{}, metricquery.ErrArithmetic
	}
	return PublicInteractionTotal{Metric: key, Event: column.Tags[model.PublicInteractionTagEvent],
		Surface: column.Tags[model.PublicInteractionTagSurface], Reason: column.Tags[model.PublicInteractionTagReason],
		Total: column.Values[0].Float64}, nil
}

func (s *MTSStore) queryPublicInteractionTotals(ctx context.Context, key string, from, to time.Time) ([]mts.ColumnSeries, error) {
	builder := mts.NewQuery().From("beat", "", key).Select(model.PublicInteractionSampleField).
		TimeRange(from.UnixNano(), to.UnixNano()).Aggregate(mts.AggregateSum, model.PublicInteractionSampleField).
		Precision(mts.PrecisionNanosecond).Limit(maximumPublicInteractionSeries + 1)
	if key == model.PublicInteractionTotalMetric {
		builder = builder.GroupByTags(model.PublicInteractionTagEvent, model.PublicInteractionTagSurface)
	} else {
		builder = builder.GroupByTags(model.PublicInteractionTagReason)
	}
	built, err := builder.Build()
	if err != nil {
		return nil, metricquery.ErrInvalid
	}
	built.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts,
		MaxSamples: metricquery.MaximumScannedSamples}
	columns, err := s.engine.QueryColumns(ctx, built)
	if err != nil {
		return nil, metricQueryReadError(err)
	}
	if len(columns) > maximumPublicInteractionSeries {
		return nil, metricquery.ErrCapacity
	}
	return columns, nil
}
