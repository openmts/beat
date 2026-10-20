package store

import (
	"context"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricquery"
)

// BucketedMetricRange 是一次公共历史读取：固定桶宽、固定桶数、每个指标一个聚合函数。
// 聚合在读取迭代过程中完成，因此 MTS 只返回预算内的样本，不做“取全部原始行再在 Go 里
// 降采样”。
type BucketedMetricRange struct {
	Metrics      []string
	Aggregations map[string]string
	NodeID       string
	From, To     time.Time
	Interval     time.Duration
	Buckets      int
}

// QueryBucketedMetrics 按请求的桶计划读取公共历史，返回每个指标的有值桶。
func (s *MTSStore) QueryBucketedMetrics(ctx context.Context, request BucketedMetricRange) (map[string][]TimePoint, error) {
	if err := validateBucketedMetricRange(request); err != nil {
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
	result := make(map[string][]TimePoint, len(request.Metrics))
	for _, metric := range request.Metrics {
		scan := metricquery.Scan{NodeID: request.NodeID, Metric: metric, Aggregation: request.Aggregations[metric],
			From: request.From, To: request.To, Interval: request.Interval, Buckets: request.Buckets}
		buckets := metricquery.NewBuckets(scan)
		if err := s.readMetricQueryRange(ctx, scan, buckets); err != nil {
			return nil, err
		}
		points, err := buckets.Points()
		if err != nil {
			return nil, err
		}
		result[metric] = bucketedTimePoints(points)
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	return result, nil
}

func validateBucketedMetricRange(request BucketedMetricRange) error {
	if len(request.Metrics) == 0 || len(request.Metrics) > metricquery.MaximumSeries ||
		!request.To.After(request.From) || request.To.Sub(request.From) > metricquery.MaximumRange ||
		request.Interval < metricquery.MinimumBucketInterval ||
		request.Buckets < 1 || request.Buckets > metricquery.MaximumPoints {
		return metricquery.ErrInvalid
	}
	seen := make(map[string]bool, len(request.Metrics))
	for _, metric := range request.Metrics {
		if seen[metric] || !metriccatalog.ValidKey(metric) {
			return metricquery.ErrInvalid
		}
		seen[metric] = true
	}
	return nil
}

func bucketedTimePoints(points []metricquery.Point) []TimePoint {
	result := make([]TimePoint, 0, len(points))
	for _, point := range points {
		if point.Value == nil {
			continue
		}
		result = append(result, TimePoint{Timestamp: time.UnixMilli(point.Timestamp).UTC(), Value: *point.Value})
	}
	return result
}
