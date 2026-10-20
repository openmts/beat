package store

import (
	"context"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// TrafficCycleSource 返回参与“活动账期保护”的节点快照。实现必须是只读查询，
// 因为它会在持有 MTS 维护锁时被调用。
type TrafficCycleSource func(ctx context.Context) ([]model.Node, error)

// WithTrafficCycleSource 注入账期保护来源。未注入时流量增量保留维持历史行为，
// 方便离线维护工具与既有调用方继续工作。
func (s *MTSStore) WithTrafficCycleSource(source TrafficCycleSource) *MTSStore {
	s.trafficCycles = source
	return s
}

// protectedTrafficCutoff 把流量增量的删除下界限制在最早活动账期起点内。
//
// 活动账期内的原始增量点参与当前周期结算：删掉它们会让已生效的校准目标失去
// “目标值 + 该秒之后的原始增量”这一对账依据，所以保留策略必须拒绝跨过账期起点。
func (s *MTSStore) protectedTrafficCutoff(
	ctx context.Context,
	cutoff time.Time,
	now time.Time,
) (time.Time, error) {
	if s.trafficCycles == nil {
		return cutoff, nil
	}
	nodes, err := s.trafficCycles(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("load traffic cycle guard: %w", err)
	}
	if oldest, found := oldestActiveCycleStart(nodes, now); found && cutoff.After(oldest) {
		return oldest, nil
	}
	return cutoff, nil
}

// oldestActiveCycleStart 返回所有节点账期起点中的最小值；没有节点时返回 false。
func oldestActiveCycleStart(nodes []model.Node, now time.Time) (time.Time, bool) {
	var oldest time.Time
	for _, node := range nodes {
		start, _ := model.BillingPeriod(now, node.TrafficResetDay)
		if oldest.IsZero() || start.Before(oldest) {
			oldest = start
		}
	}
	return oldest, !oldest.IsZero()
}

func isTrafficDeltaMeasurement(measurement string) bool {
	return measurement == trafficReceivedDelta || measurement == trafficSentDelta
}
