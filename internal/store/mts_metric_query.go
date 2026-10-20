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

const metricQueryMaximumShards = 4096

const metricQueryMaximumParts = 32768

// 边缘探测只服务于两个附带目的：序列的 available_from 提示，以及 rate 首个分桶的前一个观测。
// 它过去用「全历史 + limit 1」查询，代价与整段历史成正比（一千万样本级实测约 0.7ms/千样本）：
// 一旦某个测量超过样本预算，探测自己就会耗尽预算，于是同一测量的**每次**查询都以
// metric query capacity exceeded 失败，哪怕请求区间只有一分钟。这里给探测一份独立且更小的预算：
// 取硬预算的四分之一，既覆盖「2 秒上报、约 6 天」之内的实例（这些实例的 available_from 与过去
// 完全一致），又保证探测耗光时只按「未知」处理，绝不牵连同一次查询真正要读的区间。
var metricQueryEdgeSampleBudget = metricquery.MaximumScannedSamples / 4

// errMetricQueryEdgeBudgeted 表示边缘探测用光了自己的预算。调用方把它当作「探测不到」，
// 而不是把整次查询判失败。
var errMetricQueryEdgeBudgeted = errors.New("metric query edge probe exceeded its sample budget")

func (s *MTSStore) ReadMetricQuery(ctx context.Context, scan metricquery.Scan) (metricquery.Values, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return metricquery.Values{}, err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return metricquery.Values{}, err
	}
	if err := ctx.Err(); err != nil {
		return metricquery.Values{}, err
	}
	buckets := metricquery.NewBuckets(scan)
	if _, err := buckets.Points(); err != nil {
		return metricquery.Values{}, err
	}
	edgeScan := scan
	edgeScan.From, edgeScan.To = time.Unix(0, 0), time.Unix(0, math.MaxInt64)
	first, err := s.metricQueryAvailableFrom(ctx, edgeScan)
	if err != nil {
		return metricquery.Values{}, err
	}
	if err := s.seedMetricQueryRate(ctx, scan, buckets); err != nil {
		return metricquery.Values{}, err
	}
	if err := s.readMetricQueryRange(ctx, scan, buckets); err != nil {
		return metricquery.Values{}, err
	}
	return s.finishMetricQuery(ctx, buckets, first)
}

func (s *MTSStore) finishMetricQuery(ctx context.Context, buckets *metricquery.Buckets, first *TimePoint) (metricquery.Values, error) {
	if err := ctx.Err(); err != nil {
		return metricquery.Values{}, err
	}
	points, err := buckets.Points()
	if err != nil {
		return metricquery.Values{}, err
	}
	if err := s.ensureOpen(); err != nil {
		return metricquery.Values{}, err
	}
	result := metricquery.Values{Points: points}
	if first != nil {
		result.AvailableFrom = &first.Timestamp
	}
	return result, nil
}

func (s *MTSStore) seedMetricQueryRate(ctx context.Context, scan metricquery.Scan, buckets *metricquery.Buckets) error {
	if scan.Aggregation != "rate" || !scan.From.After(time.Unix(0, 0)) {
		return nil
	}
	scan.To = scan.From.Add(-time.Nanosecond)
	scan.From = time.Unix(0, 0)
	previous, err := s.readMetricQueryEdge(ctx, scan, true)
	if err != nil && !errors.Is(err, errMetricQueryEdgeBudgeted) {
		return err
	}
	if errors.Is(err, errMetricQueryEdgeBudgeted) {
		// 探测不到前一个观测时不再种子：首个分桶按「没有前值」留空，与真的没有前值时一致。
		return nil
	}
	if previous == nil {
		return nil
	}
	return buckets.Add(previous.Timestamp, previous.Value)
}

// metricQueryAvailableFrom 返回序列最早样本，带 60s TTL 缓存。探测够不到（无数据或预算耗尽）时
// 返回 nil：available_from 保持 null（契约允许），请求区间本身仍照常返回。
func (s *MTSStore) metricQueryAvailableFrom(ctx context.Context, scan metricquery.Scan) (*TimePoint, error) {
	now := time.Now()
	key := metricQueryEdgeKey(scan)
	if entry, ok := s.queryEdgeCache.get(key, now); ok {
		if !entry.found {
			return nil, nil
		}
		point := entry.point
		return &point, nil
	}
	first, err := s.readMetricQueryEdge(ctx, scan, false)
	if errors.Is(err, errMetricQueryEdgeBudgeted) {
		first, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry := metricQueryEdgeEntry{expires: now.Add(metricQueryEdgeCacheTTL)}
	if first != nil {
		entry.found, entry.point = true, *first
	}
	s.queryEdgeCache.put(key, entry, now)
	return first, nil
}

func metricQuery(scan metricquery.Scan) (mts.Query, error) {
	builder := mts.NewQuery().From("beat", "", scan.Metric).Select("value").
		TimeRange(scan.From.UnixNano(), scan.To.UnixNano()).Where(mts.TagEq("node", scan.NodeID)).
		Precision(mts.PrecisionNanosecond).OrderByTimeAsc()
	if model.ValidAcceleratorMetric(scan.Metric) {
		if !model.ValidAcceleratorDeviceID(scan.DeviceID) || !model.ValidAcceleratorVendor(scan.Vendor) {
			return mts.Query{}, metricquery.ErrInvalid
		}
		builder.Where(mts.TagEq("device", scan.DeviceID), mts.TagEq("vendor", scan.Vendor))
	} else if scan.DeviceID != "" {
		return mts.Query{}, metricquery.ErrInvalid
	}
	query, err := builder.Build()
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: metricquery.MaximumScannedSamples}
	return query, err
}

func (s *MTSStore) readMetricQueryEdge(ctx context.Context, scan metricquery.Scan, descending bool) (*TimePoint, error) {
	query, err := metricQuery(scan)
	if err != nil {
		return nil, err
	}
	query.Limit = 1
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts,
		MaxSamples: metricQueryEdgeSampleBudget}
	if descending {
		query.Order = mts.QueryOrder{By: mts.QueryOrderByTime, Direction: mts.QuerySortDesc}
	}
	rows, err := s.engine.QueryRows(ctx, query)
	if errors.Is(err, mts.ErrReadBudgetExceeded) {
		return nil, errMetricQueryEdgeBudgeted
	}
	if err != nil {
		return nil, metricQueryReadError(err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return metricQueryPoint(rows[0])
}

func (s *MTSStore) readMetricQueryRange(ctx context.Context, scan metricquery.Scan, buckets *metricquery.Buckets) error {
	query, err := metricQuery(scan)
	if err != nil {
		return err
	}
	iterator, err := s.engine.QueryRowIterator(ctx, query)
	if err != nil {
		return metricQueryReadError(err)
	}
	readErr := consumeMetricQuery(ctx, iterator, buckets)
	closeErr := iterator.Close()
	return metricQueryReadError(errors.Join(readErr, closeErr))
}

func consumeMetricQuery(ctx context.Context, iterator mts.RowIterator, buckets *metricquery.Buckets) error {
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		point, err := metricQueryPoint(iterator.Row())
		if err != nil {
			return err
		}
		if err := buckets.Add(point.Timestamp, point.Value); err != nil {
			return err
		}
	}
	return iterator.Err()
}

func metricQueryPoint(row mts.Row) (*TimePoint, error) {
	value, ok := row.Fields["value"]
	if !ok || value.Type != mts.FieldFloat64 || math.IsNaN(value.Float64) || math.IsInf(value.Float64, 0) {
		return nil, fmt.Errorf("metric storage returned an invalid scalar sample")
	}
	return &TimePoint{Timestamp: time.Unix(0, row.Timestamp).UTC(), Value: value.Float64}, nil
}

func metricQueryReadError(err error) error {
	if errors.Is(err, mts.ErrReadBudgetExceeded) {
		return errors.Join(metricquery.ErrCapacity, err)
	}
	return err
}
