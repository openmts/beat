package store

import (
	"context"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

// ReadMetricLatest 返回某节点某指标的当前原始样本。窗口探测命中即停，节点长期
// 离线时才回退到全历史扫描；没有样本时返回 Found=false 而不是错误。
func (s *MTSStore) ReadMetricLatest(ctx context.Context, scan metricquery.LatestScan) (metricquery.LatestValue, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return metricquery.LatestValue{}, err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return metricquery.LatestValue{}, err
	}
	if err := ctx.Err(); err != nil {
		return metricquery.LatestValue{}, err
	}
	point, err := s.queryLatestScalarPoint(ctx, latestScalarQuery{nodeID: scan.NodeID, metric: scan.Metric, end: time.Now().Add(model.MaximumAgentClockSkew)})
	if err != nil {
		return metricquery.LatestValue{}, err
	}
	if point == nil {
		return metricquery.LatestValue{}, nil
	}
	return metricquery.LatestValue{Found: true, SampleAt: point.Timestamp, Value: point.Value}, nil
}
