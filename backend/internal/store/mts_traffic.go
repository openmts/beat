package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

const (
	trafficReceivedDelta = "net_recv_delta"
	trafficSentDelta     = "net_sent_delta"
)

func (s *MTSStore) writeNodeMetrics(ctx context.Context, sample NodeMetricSample) error {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	lock := s.nodeLock(sample.NodeID)
	lock.Lock()
	defer lock.Unlock()

	received, err := s.counterDelta(
		ctx, sample.NodeID, "net_recv_total", sample.Metrics.NetRecvTotal, sample.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("derive received traffic: %w", err)
	}
	sent, err := s.counterDelta(
		ctx, sample.NodeID, "net_sent_total", sample.Metrics.NetSentTotal, sample.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("derive sent traffic: %w", err)
	}
	values := append(sample.Metrics.TimeSeries(),
		model.MetricValue{Name: trafficReceivedDelta, Value: received},
		model.MetricValue{Name: trafficSentDelta, Value: sent},
	)
	return s.writeMetricValuesLocked(ctx, sample.NodeID, values, sample.Timestamp)
}

func (s *MTSStore) QueryTrafficUsage(
	ctx context.Context,
	nodeID string,
	start time.Time,
	end time.Time,
) (model.TrafficTotals, error) {
	received, trackedSince, err := s.queryTrafficMetric(
		ctx, nodeID, trafficReceivedDelta, start, end, true,
	)
	if err != nil {
		return model.TrafficTotals{}, fmt.Errorf("query received traffic: %w", err)
	}
	sent, _, err := s.queryTrafficMetric(ctx, nodeID, trafficSentDelta, start, end, false)
	if err != nil {
		return model.TrafficTotals{}, fmt.Errorf("query sent traffic: %w", err)
	}
	return model.TrafficTotals{Sent: sent, Received: received, TrackedSince: trackedSince}, nil
}

func (s *MTSStore) counterDelta(
	ctx context.Context,
	nodeID string,
	metric string,
	current float64,
	timestamp time.Time,
) (float64, error) {
	previous, found, err := s.queryLatestMetric(ctx, nodeID, metric, timestamp)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	if current >= previous {
		return current - previous, nil
	}
	return current, nil
}

func (s *MTSStore) queryLatestMetric(
	ctx context.Context,
	nodeID string,
	metric string,
	end time.Time,
) (float64, bool, error) {
	query, err := mts.NewQuery().
		From("beat", "", metric).
		Select("value").
		TimeRange(0, end.Unix()).
		Where(mts.TagEq("node", nodeID)).
		Precision(mts.PrecisionSecond).
		OrderByTimeDesc().
		Limit(1).
		Build()
	if err != nil {
		return 0, false, fmt.Errorf("build latest metric query: %w", err)
	}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return 0, false, fmt.Errorf("query latest metric: %w", err)
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	value, ok := rows[0].Fields["value"]
	if !ok {
		return 0, false, nil
	}
	return value.Float64, true, nil
}

func (s *MTSStore) queryTrafficMetric(
	ctx context.Context,
	nodeID string,
	metric string,
	start time.Time,
	end time.Time,
	includeTrackedSince bool,
) (float64, *time.Time, error) {
	total, err := s.queryMetricSum(ctx, nodeID, metric, start, end)
	if err != nil || !includeTrackedSince {
		return total, nil, err
	}
	trackedSince, err := s.queryFirstMetricTime(ctx, nodeID, metric, start, end)
	return total, trackedSince, err
}

func (s *MTSStore) queryMetricSum(
	ctx context.Context,
	nodeID string,
	metric string,
	start time.Time,
	end time.Time,
) (float64, error) {
	query, err := mts.NewQuery().
		From("beat", "", metric).
		Aggregate(mts.AggregateSum, "value").
		TimeRange(start.Unix(), end.Unix()).
		Where(mts.TagEq("node", nodeID)).
		Precision(mts.PrecisionSecond).
		Build()
	if err != nil {
		return 0, fmt.Errorf("build metric sum query: %w", err)
	}
	columns, err := s.engine.QueryColumns(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query metric sum: %w", err)
	}
	for _, column := range columns {
		if column.FieldName == "sum(value)" && len(column.Values) > 0 {
			return column.Values[0].Float64, nil
		}
	}
	return 0, nil
}

func (s *MTSStore) queryFirstMetricTime(
	ctx context.Context,
	nodeID string,
	metric string,
	start time.Time,
	end time.Time,
) (*time.Time, error) {
	query, err := mts.NewQuery().
		From("beat", "", metric).
		Select("value").
		TimeRange(start.Unix(), end.Unix()).
		Where(mts.TagEq("node", nodeID)).
		Precision(mts.PrecisionSecond).
		OrderByTimeAsc().
		Limit(1).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build first metric query: %w", err)
	}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query first metric: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	trackedSince := time.Unix(rows[0].Timestamp, 0).UTC()
	return &trackedSince, nil
}

func (s *MTSStore) nodeLock(nodeID string) *sync.Mutex {
	lock, _ := s.nodeLocks.LoadOrStore(nodeID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
