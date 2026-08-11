package model

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestNetworkTaskValidate(t *testing.T) {
	valid := NetworkTask{
		Name: "Primary API", Type: NetworkProbeHTTP, Target: "https://example.com/health",
		IPFamily: IPFamilyAuto, IntervalSeconds: 60, TimeoutMilliseconds: 3000,
	}
	tests := []struct {
		name    string
		mutate  func(*NetworkTask)
		wantErr bool
	}{
		{name: "valid"},
		{name: "empty name", mutate: func(task *NetworkTask) { task.Name = " " }, wantErr: true},
		{name: "long name", mutate: func(task *NetworkTask) { task.Name = strings.Repeat("界", 101) }, wantErr: true},
		{name: "invalid type", mutate: func(task *NetworkTask) { task.Type = "udp" }, wantErr: true},
		{name: "invalid family", mutate: func(task *NetworkTask) { task.IPFamily = "any" }, wantErr: true},
		{name: "short interval", mutate: func(task *NetworkTask) { task.IntervalSeconds = 9 }, wantErr: true},
		{name: "long interval", mutate: func(task *NetworkTask) { task.IntervalSeconds = 86401 }, wantErr: true},
		{name: "short timeout", mutate: func(task *NetworkTask) { task.TimeoutMilliseconds = 99 }, wantErr: true},
		{name: "timeout exceeds interval", mutate: func(task *NetworkTask) { task.TimeoutMilliseconds = 11000; task.IntervalSeconds = 10 }, wantErr: true},
		{name: "negative sort", mutate: func(task *NetworkTask) { task.SortOrder = -1 }, wantErr: true},
		{name: "invalid ICMP", mutate: func(task *NetworkTask) { task.Type = NetworkProbeICMP; task.Target = "host:80" }, wantErr: true},
		{name: "valid TCP IPv6", mutate: func(task *NetworkTask) { task.Type = NetworkProbeTCP; task.Target = "[::1]:443" }},
		{name: "invalid TCP", mutate: func(task *NetworkTask) { task.Type = NetworkProbeTCP; task.Target = "example.com" }, wantErr: true},
		{name: "HTTP credentials", mutate: func(task *NetworkTask) { task.Target = "https://user:pass@example.com" }, wantErr: true},
		{name: "unsupported URL", mutate: func(task *NetworkTask) { task.Target = "ftp://example.com" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := valid
			if tt.mutate != nil {
				tt.mutate(&task)
			}
			if gotErr := task.Validate() != nil; gotErr != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", task.Validate(), tt.wantErr)
			}
		})
	}
}

func TestNetworkProbeResultValidate(t *testing.T) {
	valid := NetworkProbeResult{
		TaskID: "task-1", FinishedAt: time.Now().UTC(), LatencyMS: 1.25,
		Success: true, StatusCode: 204, ErrorCode: "none",
	}
	tests := []struct {
		name   string
		mutate func(*NetworkProbeResult)
	}{
		{name: "missing task", mutate: func(result *NetworkProbeResult) { result.TaskID = "" }},
		{name: "missing time", mutate: func(result *NetworkProbeResult) { result.FinishedAt = time.Time{} }},
		{name: "negative latency", mutate: func(result *NetworkProbeResult) { result.LatencyMS = -1 }},
		{name: "nan latency", mutate: func(result *NetworkProbeResult) { result.LatencyMS = math.NaN() }},
		{name: "bad status", mutate: func(result *NetworkProbeResult) { result.StatusCode = 1000 }},
		{name: "unknown error", mutate: func(result *NetworkProbeResult) { result.ErrorCode = "unknown" }},
		{name: "success with error", mutate: func(result *NetworkProbeResult) { result.ErrorCode = "timeout" }},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid result: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valid
			tt.mutate(&result)
			if err := result.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
