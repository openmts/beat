package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/openmts/mts"
)

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
	latest := NetworkProbeLatest{
		Timestamp: time.Unix(0, row.Timestamp).UTC(), LatencyMS: latency.Float64,
		Success: success.Float64 == 1, StatusCode: status.Int64, ErrorCode: errorCode.String,
	}
	if sent, ok := row.Fields["sent_packets"]; ok {
		latest.SentPackets = sent.Int64
		if sent.Type == mts.FieldFloat64 {
			latest.SentPackets = int64(sent.Float64)
		}
	}
	if received, ok := row.Fields["received_packets"]; ok {
		latest.ReceivedPackets = received.Int64
		if received.Type == mts.FieldFloat64 {
			latest.ReceivedPackets = int64(received.Float64)
		}
	}
	latest.LossPercent = optionalRowFloat(row, "loss_percent")
	latest.JitterMS = optionalRowFloat(row, "jitter_ms")
	latest.MinLatencyMS = optionalRowFloat(row, "min_latency_ms")
	latest.MaxLatencyMS = optionalRowFloat(row, "max_latency_ms")
	return latest, nil
}

func optionalRowFloat(row mts.Row, field string) *float64 {
	value, ok := row.Fields[field]
	if !ok {
		return nil
	}
	number := value.Float64
	return &number
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

// queryNetworkProbeRows 在给定范围内按时间倒序取一条网络探测记录。
func (s *MTSStore) queryNetworkProbeRows(ctx context.Context, taskID, nodeID string, from, to time.Time) ([]mts.Row, error) {
	query, err := networkProbeQuery(taskID, nodeID, from, to).OrderByTimeDesc().Limit(1).Build()
	if err != nil {
		return nil, fmt.Errorf("build latest network probe query: %w", err)
	}
	return s.engine.QueryRows(ctx, query)
}
