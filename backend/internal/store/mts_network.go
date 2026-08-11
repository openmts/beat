package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/openmts/mts"
)

const (
	networkProbeMeasurement = "network_probe"
	maxNetworkHistoryPoints = 600
)

type NetworkProbeSample struct {
	TaskID     string
	NodeID     string
	TaskType   string
	FinishedAt time.Time
	LatencyMS  float64
	Success    bool
	StatusCode int
	ErrorCode  string
}

type NetworkProbePoint struct {
	Timestamp      time.Time `json:"timestamp"`
	AverageLatency float64   `json:"average_latency_ms"`
	SuccessPercent float64   `json:"success_percent"`
	SampleCount    int64     `json:"sample_count"`
}

type NetworkProbeLatest struct {
	Timestamp  time.Time `json:"timestamp"`
	LatencyMS  float64   `json:"latency_ms"`
	Success    bool      `json:"success"`
	StatusCode int64     `json:"status_code"`
	ErrorCode  string    `json:"error_code"`
}

func (s *MTSStore) WriteNetworkProbes(ctx context.Context, samples []NetworkProbeSample) error {
	if len(samples) == 0 {
		return nil
	}
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	points := make([]mts.Point, 0, len(samples))
	for _, sample := range samples {
		success := 0.0
		if sample.Success {
			success = 1.0
		}
		points = append(points, mts.Point{
			Measurement: networkProbeMeasurement,
			Tags: map[string]string{
				"task": sample.TaskID, "node": sample.NodeID, "type": sample.TaskType,
			},
			Fields: map[string]mts.FieldValue{
				"latency_ms":  mts.Float64Value(sample.LatencyMS),
				"success":     mts.Float64Value(success),
				"status_code": mts.Int64Value(int64(sample.StatusCode)),
				"error_code":  mts.StringValue(sample.ErrorCode),
			},
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
	query, err := networkProbeQuery(taskID, nodeID, time.Unix(0, 0), time.Now().UTC().Add(time.Minute)).
		OrderByTimeDesc().Limit(1).Build()
	if err != nil {
		return nil, fmt.Errorf("build latest network probe query: %w", err)
	}
	rows, err := s.engine.QueryRows(ctx, query)
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
		Select("latency_ms", "success", "status_code", "error_code").
		TimeRangeTime(start, end).
		Where(mts.TagEq("task", taskID), mts.TagEq("node", nodeID))
}

func rawNetworkPoints(rows []mts.Row) ([]NetworkProbePoint, error) {
	points := make([]NetworkProbePoint, 0, len(rows))
	for _, row := range rows {
		latency, ok := row.Fields["latency_ms"]
		if !ok {
			return nil, errors.New("network probe row is missing latency_ms")
		}
		success, ok := row.Fields["success"]
		if !ok {
			return nil, errors.New("network probe row is missing success")
		}
		points = append(points, NetworkProbePoint{
			Timestamp: time.Unix(0, row.Timestamp).UTC(), AverageLatency: latency.Float64,
			SuccessPercent: success.Float64 * 100, SampleCount: 1,
		})
	}
	return points, nil
}

func latestFromRow(row mts.Row) (NetworkProbeLatest, error) {
	latency, latencyOK := row.Fields["latency_ms"]
	success, successOK := row.Fields["success"]
	status, statusOK := row.Fields["status_code"]
	errorCode, errorOK := row.Fields["error_code"]
	if !latencyOK || !successOK || !statusOK || !errorOK {
		return NetworkProbeLatest{}, errors.New("network probe row is incomplete")
	}
	return NetworkProbeLatest{
		Timestamp: time.Unix(0, row.Timestamp).UTC(), LatencyMS: latency.Float64,
		Success: success.Float64 == 1, StatusCode: status.Int64, ErrorCode: errorCode.String,
	}, nil
}

func networkHistoryWindow(duration time.Duration) time.Duration {
	if duration <= 0 {
		return time.Second
	}
	window := duration / maxNetworkHistoryPoints
	if duration%maxNetworkHistoryPoints != 0 {
		window++
	}
	if window < time.Second {
		return time.Second
	}
	return window
}

func networkPointsFromColumns(columns []mts.ColumnSeries) []NetworkProbePoint {
	pointsByTime := map[int64]NetworkProbePoint{}
	for _, column := range columns {
		for index, timestamp := range column.Timestamps {
			point := pointsByTime[timestamp]
			point.Timestamp = time.Unix(0, timestamp).UTC()
			value := column.Values[index]
			switch column.FieldName {
			case "avg(latency_ms)":
				point.AverageLatency = value.Float64
			case "avg(success)":
				point.SuccessPercent = value.Float64 * 100
			case "count(latency_ms)":
				point.SampleCount = value.Int64
				if value.Type == mts.FieldFloat64 {
					point.SampleCount = int64(value.Float64)
				}
			}
			pointsByTime[timestamp] = point
		}
	}
	timestamps := make([]int64, 0, len(pointsByTime))
	for timestamp := range pointsByTime {
		timestamps = append(timestamps, timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	points := make([]NetworkProbePoint, 0, len(timestamps))
	for _, timestamp := range timestamps {
		points = append(points, pointsByTime[timestamp])
	}
	return points
}
