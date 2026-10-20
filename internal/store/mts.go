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

const mtsCompactionConcurrency = 1

type TimePoint struct {
	Timestamp time.Time
	Value     float64
}

type MTSStore struct {
	engine          *trackedMTSEngine
	owner           *mtsOwnerLease
	path            string
	nodeLocks       sync.Map
	operationMu     sync.RWMutex
	closed          bool
	catalog         *MetricCatalogStore
	trafficCycles   TrafficCycleSource
	agentPreparedMu sync.Mutex
	agentPrepared   map[string]*preparedAgentReport
	agentStats      agentIngestCounters
	// exportBudgetOverride 只供测试注入更小的导出预算，0 表示使用归档契约上限。
	exportBudgetOverride int64
	// queryEdgeCache 缓存 available_from 的边缘探测结论，带 60s TTL，见 mts_metric_query_edge_cache.go。
	queryEdgeCache metricQueryEdgeCache
}

type NodeMetricSample struct {
	NodeID    string
	Metrics   model.NodeMetrics
	Timestamp time.Time
}

func NewMTSStore(path string) (*MTSStore, error) {
	owner, err := acquireMTSOwnerLease(path)
	if err != nil {
		return nil, err
	}
	engine, err := openMTSEngine(path)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	return &MTSStore{engine: &trackedMTSEngine{Engine: engine}, owner: owner, path: path}, nil
}

func openMTSEngine(path string) (*mts.Engine, error) {
	opts := mts.DefaultOptions(path)
	opts.DefaultDatabase = "beat"
	// 上游默认禁用整理；重叠分片会使健康检查持续失败，即使仍能读写。
	opts.Compaction.Enabled = true
	opts.Compaction.MaxConcurrent = mtsCompactionConcurrency

	engine, err := mts.Open(context.Background(), opts)
	if err != nil {
		return nil, fmt.Errorf("opening mts engine: %w", err)
	}
	// 恢复时可能读到上次未确认的完整 WAL 帧；在任何重复 ACK 前建立耐久边界。
	if err := engine.Flush(context.Background()); err != nil {
		closeErr := engine.Close(context.Background())
		return nil, fmt.Errorf("stabilizing recovered MTS: %w", errors.Join(err, closeErr))
	}
	return engine, nil
}

var knownMetrics = model.MetricNames()

func (s *MTSStore) WriteMetric(ctx context.Context, nodeID string, metric string, value float64, timestamp time.Time) error {
	if model.ValidAcceleratorMetric(metric) || metric == model.AcceleratorSampleMeasurement {
		return ErrMetricIngestion
	}
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
	if s.closed || s.engine.closed.Load() {
		return ErrMTSStoreClosed
	}
	if s.engine.poisoned.Load() {
		return ErrMTSRecoveryRequired
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

func (s *MTSStore) QueryLatest(ctx context.Context, nodeID string) (map[string]float64, error) {
	return s.QueryLatestFiltered(ctx, nodeID, nil)
}

func (s *MTSStore) QueryLatestFiltered(ctx context.Context, nodeID string, allowed map[string]bool) (map[string]float64, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.queryLatestFilteredLocked(ctx, nodeID, allowed)
}

func (s *MTSStore) queryLatestFilteredLocked(ctx context.Context, nodeID string, allowed map[string]bool) (map[string]float64, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	result := make(map[string]float64)
	until := time.Now().UTC().Add(model.MaximumAgentClockSkew)
	for _, metric := range knownMetrics {
		if allowed != nil && !allowed[metric] {
			continue
		}
		value, found, err := s.queryLatestScalar(ctx, latestScalarQuery{nodeID: nodeID, metric: metric, end: until})
		if err != nil {
			return nil, err
		}
		if found {
			result[metric] = value
		}
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
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
	return errors.Join(s.engine.Close(context.Background()), s.releaseOwner())
}

func (s *MTSStore) releaseOwner() error {
	owner := s.owner
	s.owner = nil
	if owner == nil {
		return nil
	}
	return owner.Close()
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
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}

	if err := s.engine.Flush(ctx); err != nil {
		return fmt.Errorf("flush MTS before cleanup: %w", err)
	}
	plans, err := s.metricRetentionPlan(ctx, cutoff)
	if err != nil {
		return err
	}
	now := model.NowUTC()
	for _, plan := range plans {
		cutoff := plan.cutoff
		if isTrafficDeltaMeasurement(plan.measurement) {
			protected, guardErr := s.protectedTrafficCutoff(ctx, cutoff, now)
			if guardErr != nil {
				return guardErr
			}
			cutoff = protected
		}
		if err := s.deleteMeasurementBefore(ctx, plan.measurement, cutoff); err != nil {
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
	measurements = append(measurements, model.AcceleratorMetricKeys...)
	measurements = append(measurements, trafficReceivedDelta, trafficSentDelta, networkProbeMeasurement,
		model.AgentReceiptMeasurement, model.AcceleratorSampleMeasurement)
	return append(measurements, model.PublicInteractionMetrics()...)
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
	if err := s.ensureOpen(); err != nil {
		return false, []string{err.Error()}
	}
	if err := s.owner.Verify(); err != nil {
		return false, []string{err.Error()}
	}
	health := s.engine.HealthSnapshot()
	return health.Healthy && health.Ready, append([]string(nil), health.Reasons...)
}
