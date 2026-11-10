package store

import (
	"context"
	"errors"
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
	TargetID        string
	NodeID          string
	TaskType        string
	Direction       string
	Carrier         string
	Region          string
	IPFamily        string
	FinishedAt      time.Time
	LatencyMS       float64
	Success         bool
	StatusCode      int
	ErrorCode       string
	SentPackets     int
	ReceivedPackets int
	AttemptCount    int
	SuccessCount    int
	SuccessPercent  *float64
	LossPercent     *float64
	JitterMS        *float64
	MinLatencyMS    *float64
	MaxLatencyMS    *float64
	P95LatencyMS    *float64
	StdDevLatencyMS *float64
}

type NetworkProbePoint struct {
	Timestamp      time.Time `json:"timestamp"`
	AverageLatency float64   `json:"average_latency_ms"`
	P95LatencyMS   *float64  `json:"p95_latency_ms,omitempty"`
	LossPercent    *float64  `json:"loss_percent,omitempty"`
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
	AttemptCount    int64     `json:"attempt_count"`
	SuccessCount    int64     `json:"success_count"`
	SuccessPercent  *float64  `json:"success_percent"`
	LossPercent     *float64  `json:"loss_percent"`
	JitterMS        *float64  `json:"jitter_ms"`
	MinLatencyMS    *float64  `json:"min_latency_ms"`
	MaxLatencyMS    *float64  `json:"max_latency_ms"`
	P95LatencyMS    *float64  `json:"p95_latency_ms"`
	StdDevLatencyMS *float64  `json:"stddev_latency_ms"`
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
			"attempt_count":    mts.Int64Value(int64(sample.AttemptCount)),
			"success_count":    mts.Int64Value(int64(sample.SuccessCount)),
		}
		if sample.SuccessPercent != nil {
			fields["success_percent"] = mts.Float64Value(*sample.SuccessPercent)
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
		if sample.P95LatencyMS != nil {
			fields["p95_latency_ms"] = mts.Float64Value(*sample.P95LatencyMS)
		}
		if sample.StdDevLatencyMS != nil {
			fields["stddev_latency_ms"] = mts.Float64Value(*sample.StdDevLatencyMS)
		}
		tags := map[string]string{
			"task": sample.TaskID, "node": sample.NodeID, "type": sample.TaskType,
			"direction": direction,
		}
		if sample.TargetID != "" {
			tags["target"] = sample.TargetID
		}
		if sample.Carrier != "" {
			tags["carrier"] = sample.Carrier
		}
		if sample.Region != "" {
			tags["region"] = sample.Region
		}
		if sample.IPFamily != "" {
			tags["ip_family"] = sample.IPFamily
		}
		points = append(points, mts.Point{
			Measurement: networkProbeMeasurement,
			Tags:        tags,
			Fields:      fields,
			Timestamp:   sample.FinishedAt.UnixNano(),
			Precision:   mts.PrecisionNanosecond,
		})
	}
	if err := s.engine.WritePointsAsTypedBatch(ctx, points, mts.WriteOptions{}); err != nil {
		if errors.Is(err, mts.ErrInvalidOptions) {
			if fallbackErr := s.engine.Write(ctx, points, mts.WriteOptions{}); fallbackErr != nil {
				return fmt.Errorf("write network probes: %w", fallbackErr)
			}
			return nil
		}
		return fmt.Errorf("write network probes: %w", err)
	}
	return nil
}

func (s *MTSStore) QueryNetworkLatest(
	ctx context.Context,
	taskID string,
	nodeID string,
) (*NetworkProbeLatest, error) {
	return s.QueryNetworkLatestForTarget(ctx, taskID, nodeID, "")
}

func (s *MTSStore) QueryNetworkLatestForTarget(
	ctx context.Context,
	taskID string,
	nodeID string,
	targetID string,
) (*NetworkProbeLatest, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	end := time.Now().UTC().Add(time.Minute)
	rows, err := scanLatestRows(ctx, latestScanPlan(time.Unix(0, 0), end, end), func(ctx context.Context, from, to time.Time) ([]mts.Row, error) {
		return s.queryNetworkProbeRows(ctx, taskID, nodeID, targetID, from, to)
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
	return s.QueryNetworkHistoryForTarget(ctx, taskID, nodeID, "", start, end)
}

func (s *MTSStore) QueryNetworkHistoryForTarget(
	ctx context.Context,
	taskID string,
	nodeID string,
	targetID string,
	start time.Time,
	end time.Time,
) ([]NetworkProbePoint, error) {
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	query, err := networkProbeQuery(taskID, nodeID, targetID, start, end).
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
	return s.queryAggregatedNetworkHistory(ctx, taskID, nodeID, targetID, start, end)
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
	targetID string,
	start time.Time,
	end time.Time,
) ([]NetworkProbePoint, error) {
	window := networkHistoryWindow(end.Sub(start))
	aggregates := []struct {
		function string
		field    string
	}{
		{function: mts.AggregateAvg, field: "latency_ms"},
		{function: mts.AggregateAvg, field: "p95_latency_ms"},
		{function: mts.AggregateAvg, field: "loss_percent"},
		{function: mts.AggregateAvg, field: "success_percent"},
		{function: mts.AggregateAvg, field: "success"},
		{function: mts.AggregateCount, field: "latency_ms"},
	}
	columns := []mts.ColumnSeries{}
	for _, aggregate := range aggregates {
		query, err := networkProbeQuery(taskID, nodeID, targetID, start, end).
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

func networkProbeQuery(taskID, nodeID, targetID string, start, end time.Time) *mts.QueryBuilder {
	return mts.NewQuery().From("beat", "", networkProbeMeasurement).
		Select("latency_ms", "success", "status_code", "error_code", "sent_packets",
			"received_packets", "attempt_count", "success_count", "success_percent",
			"loss_percent", "jitter_ms", "min_latency_ms", "max_latency_ms",
			"p95_latency_ms", "stddev_latency_ms").
		TimeRangeTime(start, end).
		Where(networkProbeConditions(taskID, nodeID, targetID)...)
}

func networkProbeConditions(taskID, nodeID, targetID string) []mts.QueryPredicate {
	conditions := []mts.QueryPredicate{mts.TagEq("task", taskID), mts.TagEq("node", nodeID)}
	if targetID != "" {
		conditions = append(conditions, mts.TagEq("target", targetID))
	}
	return conditions
}
