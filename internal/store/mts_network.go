package store

import (
	"context"
	"fmt"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

const (
	networkProbeMeasurement = "network_probe"
	maxNetworkHistoryPoints = 600
)

type NetworkProbeSample struct {
	TaskID          string
	NodeID          string
	TaskType        string
	Direction       string
	FinishedAt      time.Time
	LatencyMS       float64
	Success         bool
	StatusCode      int
	ErrorCode       string
	SentPackets     int
	ReceivedPackets int
	LossPercent     *float64
	JitterMS        *float64
	MinLatencyMS    *float64
	MaxLatencyMS    *float64
}

type NetworkProbePoint struct {
	Timestamp      time.Time `json:"timestamp"`
	AverageLatency float64   `json:"average_latency_ms"`
	SuccessPercent float64   `json:"success_percent"`
	SampleCount    int64     `json:"sample_count"`
}

type NetworkProbeLatest struct {
	Timestamp       time.Time `json:"timestamp"`
	LatencyMS       float64   `json:"latency_ms"`
	Success         bool      `json:"success"`
	StatusCode      int64     `json:"status_code"`
	ErrorCode       string    `json:"error_code"`
	SentPackets     int64     `json:"sent_packets"`
	ReceivedPackets int64     `json:"received_packets"`
	LossPercent     *float64  `json:"loss_percent"`
	JitterMS        *float64  `json:"jitter_ms"`
	MinLatencyMS    *float64  `json:"min_latency_ms"`
	MaxLatencyMS    *float64  `json:"max_latency_ms"`
}

func (s *MTSStore) WriteNetworkProbes(ctx context.Context, samples []NetworkProbeSample) error {
	if len(samples) == 0 {
		return nil
	}
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	points := make([]mts.Point, 0, len(samples))
	for _, sample := range samples {
		success := 0.0
		if sample.Success {
			success = 1.0
		}
		direction := sample.Direction
		if direction == "" {
			direction = model.NetworkDirectionAgentToTarget
		}
		fields := map[string]mts.FieldValue{
			"latency_ms":       mts.Float64Value(sample.LatencyMS),
			"success":          mts.Float64Value(success),
			"status_code":      mts.Int64Value(int64(sample.StatusCode)),
			"error_code":       mts.StringValue(sample.ErrorCode),
			"sent_packets":     mts.Int64Value(int64(sample.SentPackets)),
			"received_packets": mts.Int64Value(int64(sample.ReceivedPackets)),
		}
		if sample.LossPercent != nil {
			fields["loss_percent"] = mts.Float64Value(*sample.LossPercent)
		}
		if sample.JitterMS != nil {
			fields["jitter_ms"] = mts.Float64Value(*sample.JitterMS)
		}
		if sample.MinLatencyMS != nil {
			fields["min_latency_ms"] = mts.Float64Value(*sample.MinLatencyMS)
		}
		if sample.MaxLatencyMS != nil {
			fields["max_latency_ms"] = mts.Float64Value(*sample.MaxLatencyMS)
		}
		points = append(points, mts.Point{
			Measurement: networkProbeMeasurement,
			Tags: map[string]string{
				"task": sample.TaskID, "node": sample.NodeID, "type": sample.TaskType,
				"direction": direction,
			},
			Fields:    fields,
			Timestamp: sample.FinishedAt.UnixNano(),
			Precision: mts.PrecisionNanosecond,
		})
	}
	if err := s.engine.WritePointsAsTypedBatch(ctx, points, mts.WriteOptions{}); err != nil {
		return fmt.Errorf("write network probes: %w", err)
	}
	return nil
}

func (s *MTSStore) QueryNetworkLatest(
	ctx context.Context,
	taskID string,
	nodeID string,
) (*NetworkProbeLatest, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	end := time.Now().UTC().Add(time.Minute)
	rows, err := scanLatestRows(ctx, latestScanPlan(time.Unix(0, 0), end, end), func(ctx context.Context, from, to time.Time) ([]mts.Row, error) {
		return s.queryNetworkProbeRows(ctx, taskID, nodeID, from, to)
	})
	if err != nil {
		return nil, fmt.Errorf("query latest network probe: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	latest, err := latestFromRow(rows[0])
	if err != nil {
		return nil, err
	}
	return &latest, nil
}

func (s *MTSStore) QueryNetworkHistory(
	ctx context.Context,
	taskID string,
	nodeID string,
	start time.Time,
	end time.Time,
) ([]NetworkProbePoint, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	query, err := networkProbeQuery(taskID, nodeID, start, end).
		OrderByTimeAsc().Limit(maxNetworkHistoryPoints + 1).Build()
	if err != nil {
		return nil, fmt.Errorf("build raw network probe query: %w", err)
	}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query raw network probes: %w", err)
	}
	if len(rows) <= maxNetworkHistoryPoints {
		return rawNetworkPoints(rows)
	}
	return s.queryAggregatedNetworkHistory(ctx, taskID, nodeID, start, end)
}

func (s *MTSStore) DeleteNetworkTask(ctx context.Context, taskID string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	err := s.engine.Delete(ctx, mts.DeleteRequest{
		Database: "beat", Measurement: networkProbeMeasurement,
		Tags: map[string]string{"task": taskID}, StartTime: 0, EndTime: time.Now().Add(time.Hour).UnixNano(),
		Precision: mts.PrecisionNanosecond,
	})
	if err != nil {
		return fmt.Errorf("delete network task probes: %w", err)
	}
	return nil
}

func (s *MTSStore) queryAggregatedNetworkHistory(
	ctx context.Context,
	taskID string,
	nodeID string,
	start time.Time,
	end time.Time,
) ([]NetworkProbePoint, error) {
	window := networkHistoryWindow(end.Sub(start))
	aggregates := []struct {
		function string
		field    string
	}{
		{function: mts.AggregateAvg, field: "latency_ms"},
		{function: mts.AggregateAvg, field: "success"},
		{function: mts.AggregateCount, field: "latency_ms"},
	}
	columns := []mts.ColumnSeries{}
	for _, aggregate := range aggregates {
		query, err := networkProbeQuery(taskID, nodeID, start, end).
			Aggregate(aggregate.function, aggregate.field).
			GroupByTime(window).OrderByTimeAsc().Limit(maxNetworkHistoryPoints).Build()
		if err != nil {
			return nil, fmt.Errorf("build aggregated network probe query: %w", err)
		}
		result, err := s.engine.QueryColumns(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("query aggregated network probes: %w", err)
		}
		columns = append(columns, result...)
	}
	return networkPointsFromColumns(columns), nil
}

func networkProbeQuery(taskID, nodeID string, start, end time.Time) *mts.QueryBuilder {
	return mts.NewQuery().From("beat", "", networkProbeMeasurement).
		Select("latency_ms", "success", "status_code", "error_code", "sent_packets",
			"received_packets", "loss_percent", "jitter_ms", "min_latency_ms", "max_latency_ms").
		TimeRangeTime(start, end).
		Where(mts.TagEq("task", taskID), mts.TagEq("node", nodeID))
}
