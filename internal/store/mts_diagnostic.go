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

const maximumDiagnosticPoints = 20000

var ErrDiagnosticSampleRejected = errors.New("diagnostic samples were rejected")

// DiagnosticSampleBatch 是一次进度帧携带的有界数值批次；整批原子写入。
type DiagnosticSampleBatch struct {
	NodeID  string
	JobID   string
	At      time.Time
	Samples []model.DiagnosticSample
}

// DiagnosticQuery 只允许按固定标签过滤已登记的诊断测量。
type DiagnosticQuery struct {
	NodeID, JobID, Metric string
	From, To              time.Time
	Hop                   int
	Stat                  string
	Direction, Protocol   string
	Role                  string
	Limit                 int
}

// WriteDiagnosticSamples 把 Agent 上报的诊断数值写入 MTS；数值绝不进入 SQLite。
func (s *MTSStore) WriteDiagnosticSamples(ctx context.Context, batch DiagnosticSampleBatch) error {
	if batch.At.IsZero() {
		return ErrDiagnosticSampleRejected
	}
	if err := model.ValidateDiagnosticSamples(batch.Samples); err != nil {
		return ErrDiagnosticSampleRejected
	}
	points := make([]mts.Point, 0, len(batch.Samples))
	for _, sample := range batch.Samples {
		if sample.NodeID != batch.NodeID || sample.JobID != batch.JobID {
			return ErrDiagnosticSampleRejected
		}
		tags, err := sample.Tags()
		if err != nil {
			return ErrDiagnosticSampleRejected
		}
		for _, key := range []string{"hop", "stat", "direction", "protocol", "role"} {
			if tags[key] == "" {
				delete(tags, key)
			}
		}
		points = append(points, mts.Point{Measurement: sample.Metric, Tags: tags,
			Fields:    map[string]mts.FieldValue{model.DiagnosticSampleField: mts.Float64Value(sample.Value)},
			Timestamp: batch.At.UnixNano(), Precision: mts.PrecisionNanosecond})
	}
	if err := s.ImportPoints(ctx, points); err != nil {
		return fmt.Errorf("write diagnostic samples: %w", err)
	}
	return nil
}

// DeleteDiagnosticJob 在作业删除时先清理已登记的诊断序列；失败时作业保持可重试。
func (s *MTSStore) DeleteDiagnosticJob(ctx context.Context, jobID string) error {
	if jobID == "" || len(jobID) > model.MaximumDiagnosticReferenceBytes {
		return metricquery.ErrInvalid
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	for _, metric := range model.DiagnosticMetrics() {
		if err := s.engine.Delete(ctx, mts.DeleteRequest{Database: "beat", Measurement: metric,
			Tags: map[string]string{"job": jobID}, StartTime: 0, EndTime: time.Now().Add(time.Hour).UnixNano(),
			Precision: mts.PrecisionNanosecond}); err != nil {
			return fmt.Errorf("delete diagnostic series %s: %w", metric, err)
		}
	}
	return ctx.Err()
}

// ReadDiagnosticPoints 只读取一个作业的单一序列；MTS 失败必须向上报告。
func (s *MTSStore) ReadDiagnosticPoints(ctx context.Context, query DiagnosticQuery) ([]TimePoint, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, metricquery.QueryTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	builder := mts.NewQuery().From("beat", "", query.Metric).Select(model.DiagnosticSampleField).
		TimeRange(query.From.UnixNano(), query.To.UnixNano()).Where(mts.TagEq("node", query.NodeID), mts.TagEq("job", query.JobID)).
		Precision(mts.PrecisionNanosecond).OrderByTimeAsc().Limit(query.Limit + 1)
	for _, filter := range query.filters() {
		builder = builder.Where(filter)
	}
	built, err := builder.Build()
	if err != nil {
		return nil, metricquery.ErrInvalid
	}
	built.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts,
		MaxSamples: metricquery.MaximumScannedSamples}
	rows, err := s.engine.QueryRows(ctx, built)
	if err != nil {
		return nil, metricQueryReadError(err)
	}
	if len(rows) > query.Limit {
		return nil, metricquery.ErrCapacity
	}
	points := make([]TimePoint, 0, len(rows))
	for _, row := range rows {
		value, ok := row.Fields[model.DiagnosticSampleField]
		if !ok || value.Type != mts.FieldFloat64 || math.IsNaN(value.Float64) || math.IsInf(value.Float64, 0) {
			return nil, metricquery.ErrArithmetic
		}
		points = append(points, TimePoint{Timestamp: time.Unix(0, row.Timestamp).UTC(), Value: value.Float64})
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	return points, ctx.Err()
}

func (query DiagnosticQuery) filters() []mts.QueryPredicate {
	conditions := make([]mts.QueryPredicate, 0, 5)
	switch {
	case model.IsMTRDiagnosticMetric(query.Metric):
		conditions = append(conditions, mts.TagEq("hop", fmt.Sprintf("%d", query.Hop)), mts.TagEq("stat", query.Stat))
	case model.IsIperfDiagnosticMetric(query.Metric):
		conditions = append(conditions, mts.TagEq("direction", query.Direction),
			mts.TagEq("protocol", query.Protocol), mts.TagEq("role", query.Role))
	}
	return conditions
}

func (query DiagnosticQuery) validate() error {
	if query.NodeID == "" || query.JobID == "" || !model.ValidDiagnosticMetric(query.Metric) {
		return metricquery.ErrInvalid
	}
	if len(query.NodeID) > model.MaximumDiagnosticReferenceBytes || len(query.JobID) > model.MaximumDiagnosticReferenceBytes {
		return metricquery.ErrInvalid
	}
	if query.Limit < metricquery.MinimumPoints || query.Limit > maximumDiagnosticPoints {
		return metricquery.ErrInvalid
	}
	if !query.To.After(query.From) || query.To.Sub(query.From) > metricquery.MaximumRange {
		return metricquery.ErrInvalid
	}
	if query.From.Before(time.Unix(0, 0)) {
		return metricquery.ErrInvalid
	}
	sample := model.DiagnosticSample{JobID: query.JobID, NodeID: query.NodeID, Metric: query.Metric,
		Hop: query.Hop, Stat: query.Stat, Role: query.Role, Direction: query.Direction, Protocol: query.Protocol, Value: 1}
	if _, err := sample.Tags(); err != nil {
		return metricquery.ErrInvalid
	}
	return nil
}
