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
