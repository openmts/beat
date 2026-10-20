package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// ErrTrafficBoundaryNode 表示调用方没有提供节点标识，无法定位流量边界。
var ErrTrafficBoundaryNode = errors.New("traffic boundary requires a node")

// TrafficAccountingBoundary 返回该节点最后一个已提交流量样本的秒级边界。
//
// 调用方必须把它当作校准可用的最大生效秒：所有既有样本的秒都小于等于该边界，
// 之后的新样本（Agent 报告强制严格递增）必然落在边界之后，因此“目标 + 边界之后
// 的原始增量”既不会漏计也不会重复计数。边界来自持久的 MTS 收据，重启与恢复后
// 依旧成立，不依赖任何内存计时器。返回 found=false 表示该节点尚无任何流量样本。
func (s *MTSStore) TrafficAccountingBoundary(ctx context.Context, nodeID string) (time.Time, bool, error) {
	if nodeID == "" {
		return time.Time{}, false, ErrTrafficBoundaryNode
	}
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return time.Time{}, false, err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return time.Time{}, false, err
	}
	lock := s.nodeLock(nodeID)
	lock.Lock()
	defer lock.Unlock()
	if err := s.engine.Flush(ctx); err != nil {
		return time.Time{}, false, fmt.Errorf("flush node traffic boundary: %w", err)
	}
	receipt, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: nodeID, end: math.MaxInt64})
	if err != nil {
		return time.Time{}, false, err
	}
	if receipt != nil {
		return receipt.CapturedAt.UTC().Truncate(time.Second), true, nil
	}
	return s.lastTrafficDeltaSecond(ctx, nodeID)
}

func (s *MTSStore) lastTrafficDeltaSecond(ctx context.Context, nodeID string) (time.Time, bool, error) {
	latest := time.Time{}
	found := false
	limit := time.Now().UTC().Add(24 * time.Hour).Unix()
	for _, metric := range []string{trafficReceivedDelta, trafficSentDelta} {
		query, err := mts.NewQuery().
			From("beat", "", metric).
			Select("value").
			TimeRange(1, limit).
			Where(mts.TagEq("node", nodeID)).
			Precision(mts.PrecisionSecond).
			OrderByTimeDesc().
			Limit(1).
			Build()
		if err != nil {
			return time.Time{}, false, fmt.Errorf("build last traffic query: %w", err)
		}
		rows, err := s.engine.QueryRows(ctx, query)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("query last traffic delta: %w", err)
		}
		if len(rows) == 0 {
			continue
		}
		at := time.Unix(rows[0].Timestamp, 0).UTC()
		if !found || at.After(latest) {
			latest, found = at, true
		}
	}
	return latest, found, nil
}

// QueryTrafficUsageAfter 汇总严格晚于 after 的原始增量（半开区间 (after, end)）。
// 调用方用校准生效秒作为 after，即可得到“生效秒之后”的原始流量。
func (s *MTSStore) QueryTrafficUsageAfter(
	ctx context.Context,
	nodeID string,
	after time.Time,
	end time.Time,
) (model.TrafficTotals, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return model.TrafficTotals{}, err
	}
	lower := after.UTC()
	upper := end.UTC()
	if !lower.Before(upper) {
		return model.TrafficTotals{}, nil
	}
	received, err := s.queryTrafficSumAfter(ctx, nodeID, trafficReceivedDelta, lower, upper)
	if err != nil {
		return model.TrafficTotals{}, fmt.Errorf("query received traffic after boundary: %w", err)
	}
	sent, err := s.queryTrafficSumAfter(ctx, nodeID, trafficSentDelta, lower, upper)
	if err != nil {
		return model.TrafficTotals{}, fmt.Errorf("query sent traffic after boundary: %w", err)
	}
	start := lower
	return model.TrafficTotals{Sent: sent, Received: received, TrackedSince: &start}, nil
}

func (s *MTSStore) queryTrafficSumAfter(
	ctx context.Context,
	nodeID string,
	metric string,
	after time.Time,
	end time.Time,
) (float64, error) {
	query, err := mts.NewQuery().
		From("beat", "", metric).
		Aggregate(mts.AggregateSum, "value").
		TimeRange(after.UnixNano()+1, end.UnixNano()).
		Where(mts.TagEq("node", nodeID)).
		Precision(mts.PrecisionNanosecond).
		Build()
	if err != nil {
		return 0, fmt.Errorf("build traffic sum query: %w", err)
	}
	columns, err := s.engine.QueryColumns(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query traffic sum: %w", err)
	}
	for _, column := range columns {
		if column.FieldName == "sum(value)" && len(column.Values) > 0 {
			return column.Values[0].Float64, nil
		}
	}
	return 0, nil
}
