package store

import (
	"context"
	"testing"
	"time"
)

func TestMTSNetworkProbeWriteLatestHistoryAndDelete(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	start := time.Date(2026, 7, 29, 0, 0, 0, 123, time.UTC)
	samples := []NetworkProbeSample{
		{TaskID: "task-1", NodeID: "node-1", TaskType: "icmp", FinishedAt: start,
			LatencyMS: 1.5, Success: true, ErrorCode: "none"},
		{TaskID: "task-1", NodeID: "node-1", TaskType: "icmp", FinishedAt: start.Add(time.Minute),
			LatencyMS: 4.5, Success: false, ErrorCode: "timeout"},
		{TaskID: "task-2", NodeID: "node-1", TaskType: "tcp", FinishedAt: start,
			LatencyMS: 2.5, Success: true, ErrorCode: "none"},
	}
	if err := s.WriteNetworkProbes(ctx, samples); err != nil {
		t.Fatalf("write probes: %v", err)
	}
	latest, err := s.QueryNetworkLatest(ctx, "task-1", "node-1")
	if err != nil {
		t.Fatalf("query latest: %v", err)
	}
	if latest == nil || latest.Success || latest.LatencyMS != 4.5 || latest.ErrorCode != "timeout" {
		t.Fatalf("latest = %#v", latest)
	}
	history, err := s.QueryNetworkHistory(ctx, "task-1", "node-1", start.Add(-time.Second), start.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	if len(history) != 2 || history[0].SuccessPercent != 100 || history[1].SuccessPercent != 0 {
		t.Fatalf("history = %#v", history)
	}
	if !history[0].Timestamp.Equal(start) {
		t.Fatalf("timestamp = %v, want %v", history[0].Timestamp, start)
	}
	if err := s.DeleteNetworkTask(ctx, "task-1"); err != nil {
		t.Fatalf("delete task probes: %v", err)
	}
	history, err = s.QueryNetworkHistory(ctx, "task-1", "node-1", start.Add(-time.Second), start.Add(2*time.Minute))
	if err != nil || len(history) != 0 {
		t.Fatalf("deleted history = %#v, error = %v", history, err)
	}
	other, err := s.QueryNetworkLatest(ctx, "task-2", "node-1")
	if err != nil || other == nil {
		t.Fatalf("other task latest = %#v, error = %v", other, err)
	}
}

func TestMTSNetworkProbeDuplicateIsIdempotent(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	finished := time.Now().UTC().Truncate(time.Nanosecond)
	first := NetworkProbeSample{TaskID: "task", NodeID: "node", TaskType: "http", FinishedAt: finished,
		LatencyMS: 10, Success: true, StatusCode: 200, ErrorCode: "none"}
	second := first
	second.LatencyMS = 12
	second.StatusCode = 204
	if err := s.WriteNetworkProbes(ctx, []NetworkProbeSample{first}); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := s.WriteNetworkProbes(ctx, []NetworkProbeSample{second}); err != nil {
		t.Fatalf("write second: %v", err)
	}
	history, err := s.QueryNetworkHistory(ctx, "task", "node", finished.Add(-time.Second), finished.Add(time.Second))
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	if len(history) != 1 || history[0].AverageLatency != 12 {
		t.Fatalf("history = %#v", history)
	}
	latest, err := s.QueryNetworkLatest(ctx, "task", "node")
	if err != nil || latest == nil || latest.StatusCode != 204 {
		t.Fatalf("latest = %#v, error = %v", latest, err)
	}
}

func TestMTSNetworkProbeDirectionAndQualityFields(t *testing.T) {
	s := setupTestMTS(t)
	ctx := t.Context()
	finished := time.Now().UTC().Truncate(time.Nanosecond)
	loss, jitter, minimum, maximum := 25.0, 1.5, 3.0, 9.0
	sample := NetworkProbeSample{
		TaskID: "task-quality", NodeID: "node-quality", TaskType: "icmp",
		Direction: "server_to_agent", FinishedAt: finished, LatencyMS: 6,
		Success: true, ErrorCode: "none", SentPackets: 4, ReceivedPackets: 3,
		LossPercent: &loss, JitterMS: &jitter, MinLatencyMS: &minimum, MaxLatencyMS: &maximum,
	}
	if err := s.WriteNetworkProbes(ctx, []NetworkProbeSample{sample}); err != nil {
		t.Fatalf("write quality sample: %v", err)
	}
	rows, err := s.queryNetworkProbeRows(ctx, sample.TaskID, sample.NodeID, "", finished.Add(-time.Second), finished.Add(time.Second))
	if err != nil || len(rows) != 1 {
		t.Fatalf("quality rows = %#v, error = %v", rows, err)
	}
	if rows[0].Tags["direction"] != "server_to_agent" || rows[0].Tags["type"] != "icmp" {
		t.Fatalf("quality tags = %#v", rows[0].Tags)
	}
	for field, want := range map[string]float64{"loss_percent": loss, "jitter_ms": jitter, "min_latency_ms": minimum, "max_latency_ms": maximum} {
		if got, ok := rows[0].Fields[field]; !ok || got.Float64 != want {
			t.Fatalf("field %s = %#v, want %v", field, got, want)
		}
	}
	latest, err := s.QueryNetworkLatest(ctx, sample.TaskID, sample.NodeID)
	if err != nil || latest == nil || latest.LossPercent == nil || *latest.LossPercent != loss ||
		latest.JitterMS == nil || *latest.JitterMS != jitter || latest.SentPackets != 4 || latest.ReceivedPackets != 3 {
		t.Fatalf("latest quality = %#v, error = %v", latest, err)
	}

	plain := NetworkProbeSample{TaskID: "task-tcp", NodeID: "node-quality", TaskType: "tcp",
		FinishedAt: finished.Add(time.Second), LatencyMS: 2, Success: true, ErrorCode: "none"}
	if err := s.WriteNetworkProbes(ctx, []NetworkProbeSample{plain}); err != nil {
		t.Fatalf("write plain sample: %v", err)
	}
	plainLatest, err := s.QueryNetworkLatest(ctx, plain.TaskID, plain.NodeID)
	if err != nil || plainLatest == nil || plainLatest.LossPercent != nil || plainLatest.JitterMS != nil {
		t.Fatalf("plain latest = %#v, error = %v", plainLatest, err)
	}
}

func TestMTSNetworkProbeMixedOptionalFields(t *testing.T) {
	s := setupTestMTS(t)
	ctx := t.Context()
	start := time.Date(2026, 11, 9, 13, 22, 0, 0, time.UTC)
	successPercent, jitter, minimum, maximum, p95, stddev := 100.0, 1.5, 10.0, 20.0, 19.0, 2.0
	samples := []NetworkProbeSample{
		{TaskID: "carrier", TargetID: "mobile", NodeID: "node", TaskType: "http",
			FinishedAt: start, LatencyMS: 15, Success: true, ErrorCode: "none",
			AttemptCount: 5, SuccessCount: 5, SuccessPercent: &successPercent,
			JitterMS: &jitter, MinLatencyMS: &minimum, MaxLatencyMS: &maximum,
			P95LatencyMS: &p95, StdDevLatencyMS: &stddev},
		{TaskID: "carrier", TargetID: "unicom", NodeID: "node", TaskType: "http",
			FinishedAt: start.Add(time.Second), LatencyMS: 16, Success: false,
			StatusCode: 403, ErrorCode: "http_status", AttemptCount: 5,
			SuccessPercent: &successPercent},
	}
	if err := s.WriteNetworkProbes(ctx, samples); err != nil {
		t.Fatalf("write mixed optional fields: %v", err)
	}
	for _, sample := range samples {
		latest, err := s.QueryNetworkLatestForTarget(ctx, sample.TaskID, sample.NodeID, sample.TargetID)
		if err != nil || latest == nil || latest.Success != sample.Success {
			t.Fatalf("latest %s = %#v, error = %v", sample.TargetID, latest, err)
		}
	}
}

func TestMTSNetworkProbeTargetIsolationAndStatistics(t *testing.T) {
	s := setupTestMTS(t)
	ctx := t.Context()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	p95, stddev, success, loss := 82.0, 7.5, 66.6667, 33.3333
	samples := []NetworkProbeSample{
		{TaskID: "carrier", TargetID: "mobile", NodeID: "node", TaskType: "icmp", Carrier: "mobile",
			Region: "上海", IPFamily: "ipv4", FinishedAt: start, LatencyMS: 20, Success: true,
			ErrorCode: "none", AttemptCount: 3, SuccessCount: 2, SuccessPercent: &success,
			LossPercent: &loss, P95LatencyMS: &p95, StdDevLatencyMS: &stddev},
		{TaskID: "carrier", TargetID: "unicom", NodeID: "node", TaskType: "icmp", Carrier: "unicom",
			Region: "北京", IPFamily: "ipv4", FinishedAt: start.Add(time.Second), LatencyMS: 30,
			Success: true, ErrorCode: "none", AttemptCount: 3, SuccessCount: 3, SuccessPercent: &success,
			LossPercent: &loss, P95LatencyMS: &p95, StdDevLatencyMS: &stddev},
		{TaskID: "carrier", TargetID: "telecom", NodeID: "node", TaskType: "icmp", Carrier: "telecom",
			Region: "广州", IPFamily: "ipv6", FinishedAt: start.Add(2 * time.Second), LatencyMS: 40,
			Success: true, ErrorCode: "none", AttemptCount: 3, SuccessCount: 3, SuccessPercent: &success,
			LossPercent: &loss, P95LatencyMS: &p95, StdDevLatencyMS: &stddev},
	}
	if err := s.WriteNetworkProbes(ctx, samples); err != nil {
		t.Fatalf("write target samples: %v", err)
	}
	for _, sample := range samples {
		latest, err := s.QueryNetworkLatestForTarget(ctx, "carrier", "node", sample.TargetID)
		if err != nil || latest == nil || latest.LatencyMS != sample.LatencyMS {
			t.Fatalf("latest %s = %#v, error = %v", sample.TargetID, latest, err)
		}
		history, err := s.QueryNetworkHistoryForTarget(ctx, "carrier", "node", sample.TargetID,
			start.Add(-time.Second), start.Add(3*time.Second))
		if err != nil || len(history) != 1 || history[0].AverageLatency != sample.LatencyMS {
			t.Fatalf("history %s = %#v, error = %v", sample.TargetID, history, err)
		}
	}
	latest, err := s.QueryNetworkLatestForTarget(ctx, "carrier", "node", "mobile")
	if err != nil || latest == nil || latest.AttemptCount != 3 || latest.SuccessCount != 2 ||
		latest.SuccessPercent == nil || *latest.SuccessPercent != success ||
		latest.LossPercent == nil || *latest.LossPercent != loss ||
		latest.P95LatencyMS == nil || *latest.P95LatencyMS != p95 ||
		latest.StdDevLatencyMS == nil || *latest.StdDevLatencyMS != stddev {
		t.Fatalf("mobile statistics = %#v, error = %v", latest, err)
	}
	rows, err := s.queryNetworkProbeRows(ctx, "carrier", "node", "mobile", start.Add(-time.Second), start.Add(time.Second))
	if err != nil || len(rows) != 1 {
		t.Fatalf("mobile rows = %#v, error = %v", rows, err)
	}
	for tag, want := range map[string]string{"target": "mobile", "carrier": "mobile", "region": "上海", "ip_family": "ipv4"} {
		if got := rows[0].Tags[tag]; got != want {
			t.Fatalf("tag %s = %q, want %q", tag, got, want)
		}
	}
}

func TestMTSNetworkProbeHistoryAggregatesToLimit(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	samples := make([]NetworkProbeSample, 0, 601)
	for index := 0; index < 601; index++ {
		samples = append(samples, NetworkProbeSample{
			TaskID: "task", NodeID: "node", TaskType: "tcp",
			FinishedAt: start.Add(time.Duration(index) * 10 * time.Second),
			LatencyMS:  float64(index), Success: index%2 == 0, ErrorCode: "none",
		})
	}
	if err := s.WriteNetworkProbes(ctx, samples); err != nil {
		t.Fatalf("write probes: %v", err)
	}
	history, err := s.QueryNetworkHistory(ctx, "task", "node", start, start.Add(6010*time.Second))
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	if len(history) == 0 || len(history) > maxNetworkHistoryPoints {
		t.Fatalf("history point count = %d", len(history))
	}
	var count int64
	for _, point := range history {
		count += point.SampleCount
	}
	if count != 601 {
		t.Fatalf("sample count = %d, want 601; history = %#v", count, history[:1])
	}
}

func TestNetworkHistoryWindow(t *testing.T) {
	if got := networkHistoryWindow(0); got != time.Second {
		t.Fatalf("zero window = %v", got)
	}
	if got := networkHistoryWindow(time.Hour); got != 6*time.Second {
		t.Fatalf("hour window = %v", got)
	}
	if got := networkHistoryWindow(time.Millisecond); got != time.Second {
		t.Fatalf("sub-second window = %v", got)
	}
}

func TestQueryNetworkLatestEmptyAndWriteEmpty(t *testing.T) {
	s := setupTestMTS(t)
	ctx := context.Background()
	latest, err := s.QueryNetworkLatest(ctx, "missing-task", "node")
	if err != nil || latest != nil {
		t.Fatalf("missing task latest = %#v, err = %v", latest, err)
	}
	if err := s.WriteNetworkProbes(ctx, nil); err != nil {
		t.Fatalf("write empty probes: %v", err)
	}
}
