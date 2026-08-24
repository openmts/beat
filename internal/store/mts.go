package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// ErrMTSStoreClosed 表示存储已在关闭后被使用；调用方应将其视为不可恢复的
// 生命周期错误，而不是空结果。
var ErrMTSStoreClosed = errors.New("MTS store is closed")

type TimePoint struct {
	Timestamp time.Time
	Value     float64
}

type MTSStore struct {
	engine      *mts.Engine
	path        string
	nodeLocks   sync.Map
	operationMu sync.RWMutex
	closed      bool
}

type NodeMetricSample struct {
	NodeID    string
	Metrics   model.NodeMetrics
	Timestamp time.Time
}

func NewMTSStore(path string) (*MTSStore, error) {
	opts := mts.DefaultOptions(path)
	opts.DefaultDatabase = "beat"

	engine, err := mts.Open(context.Background(), opts)
	if err != nil {
		return nil, fmt.Errorf("opening mts engine: %w", err)
	}

	return &MTSStore{engine: engine, path: path}, nil
}

var knownMetrics = model.MetricNames()

func (s *MTSStore) WriteMetric(ctx context.Context, nodeID string, metric string, value float64, timestamp time.Time) error {
	return s.writeMetricValues(
		ctx,
		nodeID,
		[]model.MetricValue{{Name: metric, Value: value}},
		timestamp,
	)
}

func (s *MTSStore) WriteNodeMetrics(ctx context.Context, sample NodeMetricSample) error {
	return s.writeNodeMetrics(ctx, sample)
}

// ensureOpen 必须在持有 operationMu（读锁或写锁）时调用，保证与 Close 互斥。
func (s *MTSStore) ensureOpen() error {
	if s.closed {
		return ErrMTSStoreClosed
	}
	return nil
}

func (s *MTSStore) writeMetricValues(
	ctx context.Context,
	nodeID string,
	metrics []model.MetricValue,
	timestamp time.Time,
) error {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	return s.writeMetricValuesLocked(ctx, nodeID, metrics, timestamp)
}

func (s *MTSStore) writeMetricValuesLocked(
	ctx context.Context,
	nodeID string,
	metrics []model.MetricValue,
	timestamp time.Time,
) error {
	tags := map[string]string{"node": nodeID}
	points := make([]mts.Point, 0, len(metrics))
	for _, metric := range metrics {
		points = append(points, mts.Point{
			Measurement: metric.Name,
			Tags:        tags,
			Fields: map[string]mts.FieldValue{
				"value": mts.Float64Value(metric.Value),
			},
			Timestamp: timestamp.Unix(),
			Precision: mts.PrecisionSecond,
		})
	}

	err := s.engine.Write(ctx, points, mts.WriteOptions{})
	if err != nil {
		return fmt.Errorf("writing metrics for node %s: %w", nodeID, err)
	}
	return nil
}

func (s *MTSStore) QueryMetrics(
	ctx context.Context,
	metrics []string,
	start time.Time,
	end time.Time,
	nodeID string,
) (map[string][]TimePoint, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	result := map[string][]TimePoint{}
	startUnix := start.Unix()
	endUnix := end.Unix()

	for _, metric := range metrics {
		query, err := mts.NewQuery().
			From("beat", "", metric).
			Select("value").
			TimeRange(startUnix, endUnix).
			Where(mts.TagEq("node", nodeID)).
			Precision(mts.PrecisionSecond).
			OrderByTimeAsc().
			Build()
		if err != nil {
			return nil, fmt.Errorf("building query for metric %s: %w", metric, err)
		}

		rows, err := s.engine.QueryRows(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("querying metric %s: %w", metric, err)
		}

		points := []TimePoint{}
		for _, row := range rows {
			fv, ok := row.Fields["value"]
			if !ok {
				continue
			}
			points = append(points, TimePoint{
				Timestamp: time.Unix(row.Timestamp, 0),
				Value:     fv.Float64,
			})
		}
		result[metric] = points
	}

	return result, nil
}

func (s *MTSStore) QueryLatest(ctx context.Context, nodeID string) (map[string]float64, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	startUnix := start.Unix()
	endUnix := now.Unix()

	result := map[string]float64{}

	for _, metric := range knownMetrics {
		query, err := mts.NewQuery().
			From("beat", "", metric).
			Select("value").
			TimeRange(startUnix, endUnix).
			Where(mts.TagEq("node", nodeID)).
			Precision(mts.PrecisionSecond).
			OrderByTimeDesc().
			Limit(1).
			Build()
		if err != nil {
			return nil, fmt.Errorf("building latest query for metric %s: %w", metric, err)
		}

		rows, err := s.engine.QueryRows(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("querying latest metric %s: %w", metric, err)
		}

		if len(rows) > 0 {
			fv, ok := rows[0].Fields["value"]
			if ok {
				result[metric] = fv.Float64
			}
		}
	}

	return result, nil
}

func (s *MTSStore) Close() error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.engine.Close(context.Background())
}

func (s *MTSStore) Flush(ctx context.Context) error {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	return s.engine.Flush(ctx)
}

func (s *MTSStore) CleanupBefore(ctx context.Context, cutoff time.Time) error {
	if cutoff.IsZero() || cutoff.Unix() <= 0 {
		return fmt.Errorf("cleanup cutoff must be after the Unix epoch")
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}

	if err := s.engine.Flush(ctx); err != nil {
		return fmt.Errorf("flush MTS before cleanup: %w", err)
	}
	for _, measurement := range managedMeasurements() {
		if err := s.deleteMeasurementBefore(ctx, measurement, cutoff); err != nil {
			return err
		}
	}
	if _, err := s.engine.CompactWithResult(ctx); err != nil {
		return fmt.Errorf("compact MTS after cleanup: %w", err)
	}
	return nil
}

func (s *MTSStore) deleteMeasurementBefore(
	ctx context.Context,
	measurement string,
	cutoff time.Time,
) error {
	err := s.engine.Delete(ctx, mts.DeleteRequest{
		Database: "beat", Measurement: measurement, StartTime: 0,
		EndTime: cutoff.Unix(), Precision: mts.PrecisionSecond,
	})
	if err != nil {
		return fmt.Errorf("delete expired MTS measurement %s: %w", measurement, err)
	}
	return nil
}

func managedMeasurements() []string {
	measurements := append([]string(nil), model.MetricNames()...)
	return append(measurements, trafficReceivedDelta, trafficSentDelta, networkProbeMeasurement)
}

func (s *MTSStore) DiskUsage() (int64, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}

	var total int64
	err := filepath.WalkDir(s.path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure MTS disk usage: %w", err)
	}
	return total, nil
}

func (s *MTSStore) Health() (bool, []string) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if s.closed {
		return false, []string{ErrMTSStoreClosed.Error()}
	}
	health := s.engine.HealthSnapshot()
	return health.Healthy && health.Ready, append([]string(nil), health.Reasons...)
}
