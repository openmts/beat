package model

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestNetworkTaskValidate(t *testing.T) {
	valid := NetworkTask{
		Name: "Primary API", Direction: NetworkDirectionAgentToTarget, Type: NetworkProbeHTTP, Target: "https://example.com/health",
		IPFamily: IPFamilyAuto, IntervalSeconds: 60, TimeoutMilliseconds: 3000,
		PacketCount: 1,
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
		{name: "valid ICMP IPv6", mutate: func(task *NetworkTask) { task.Type = NetworkProbeICMP; task.Target = "::1" }},
		{name: "invalid direction", mutate: func(task *NetworkTask) { task.Direction = "sideways" }, wantErr: true},
		{name: "invalid packet count", mutate: func(task *NetworkTask) { task.PacketCount = 0 }, wantErr: true},
		{name: "packet count too high", mutate: func(task *NetworkTask) { task.PacketCount = 21 }, wantErr: true},
		{name: "valid TCP IPv6", mutate: func(task *NetworkTask) { task.Type = NetworkProbeTCP; task.Target = "[::1]:443" }},
		{name: "invalid TCP", mutate: func(task *NetworkTask) { task.Type = NetworkProbeTCP; task.Target = "example.com" }, wantErr: true},
		{name: "HTTP credentials", mutate: func(task *NetworkTask) { task.Target = "https://user:pass@example.com" }, wantErr: true},
		{name: "unsupported URL", mutate: func(task *NetworkTask) { task.Target = "ftp://example.com" }, wantErr: true},
		{name: "valid server ICMP", mutate: func(task *NetworkTask) {
			task.Direction = NetworkDirectionServerToAgent
			task.Type = NetworkProbeICMP
			task.Target = "{{host}}"
		}},
		{name: "valid server TCP", mutate: func(task *NetworkTask) {
			task.Direction = NetworkDirectionServerToAgent
			task.Type = NetworkProbeTCP
			task.Target = "{{host_port}}"
		}},
		{name: "valid server HTTP", mutate: func(task *NetworkTask) {
			task.Direction = NetworkDirectionServerToAgent
			task.Type = NetworkProbeHTTP
			task.Target = "https://{{host}}/health"
		}},
		{name: "server target cannot be arbitrary", mutate: func(task *NetworkTask) {
			task.Direction = NetworkDirectionServerToAgent
			task.Target = "https://example.com/"
		}, wantErr: true},
		{name: "server ICMP requires host template", mutate: func(task *NetworkTask) {
			task.Direction = NetworkDirectionServerToAgent
			task.Type = NetworkProbeICMP
			task.Target = "{{host_port}}"
		}, wantErr: true},
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

func TestNetworkTaskNormalizeDefaults(t *testing.T) {
	tests := []struct {
		name      string
		input     NetworkTask
		direction string
		target    string
		packets   int
	}{
		{name: "agent ICMP", input: NetworkTask{Type: NetworkProbeICMP}, direction: NetworkDirectionAgentToTarget, packets: DefaultNetworkPacketCount},
		{name: "agent TCP", input: NetworkTask{Type: NetworkProbeTCP}, direction: NetworkDirectionAgentToTarget, packets: 1},
		{name: "server ICMP", input: NetworkTask{Direction: NetworkDirectionServerToAgent, Type: NetworkProbeICMP}, direction: NetworkDirectionServerToAgent, target: "{{host}}", packets: DefaultNetworkPacketCount},
		{name: "server TCP", input: NetworkTask{Direction: NetworkDirectionServerToAgent, Type: NetworkProbeTCP}, direction: NetworkDirectionServerToAgent, target: "{{host_port}}", packets: 1},
		{name: "server HTTP", input: NetworkTask{Direction: NetworkDirectionServerToAgent, Type: NetworkProbeHTTP}, direction: NetworkDirectionServerToAgent, target: "http://{{host}}/", packets: 1},
		{name: "non ICMP packet count", input: NetworkTask{Type: NetworkProbeHTTP, PacketCount: 9}, direction: NetworkDirectionAgentToTarget, packets: 1},
		{name: "existing agent target", input: NetworkTask{Direction: NetworkDirectionAgentToTarget, Type: NetworkProbeICMP, Target: "example.com"}, direction: NetworkDirectionAgentToTarget, target: "example.com", packets: DefaultNetworkPacketCount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.input
			task.Normalize()
			if task.Direction != tt.direction || task.Target != tt.target || task.PacketCount != tt.packets {
				t.Fatalf("normalized task = %#v", task)
			}
		})
	}
}

func TestRenderNetworkTarget(t *testing.T) {
	tests := []struct {
		name      string
		probeType string
		target    string
		host      string
		port      int
		want      string
		wantErr   bool
	}{
		{name: "ICMP IPv4", probeType: NetworkProbeICMP, target: "{{host}}", host: "192.0.2.1", port: 22, want: "192.0.2.1"},
		{name: "ICMP IPv6", probeType: NetworkProbeICMP, target: "{{host}}", host: "2001:db8::1", port: 22, want: "2001:db8::1"},
		{name: "TCP IPv6", probeType: NetworkProbeTCP, target: "{{host_port}}", host: "2001:db8::1", port: 443, want: "[2001:db8::1]:443"},
		{name: "HTTP IPv6", probeType: NetworkProbeHTTP, target: "https://{{host}}/health", host: "2001:db8::1", port: 443, want: "https://[2001:db8::1]/health"},
		{name: "HTTP SSH port", probeType: NetworkProbeHTTP, target: "http://{{host}}:{{ssh_port}}/", host: "192.0.2.1", port: 2222, want: "http://192.0.2.1:2222/"},
		{name: "empty host", probeType: NetworkProbeICMP, target: "{{host}}", port: 22, wantErr: true},
		{name: "bad port", probeType: NetworkProbeTCP, target: "{{host_port}}", host: "192.0.2.1", port: 0, wantErr: true},
		{name: "unknown placeholder", probeType: NetworkProbeICMP, target: "{{unknown}}", host: "192.0.2.1", port: 22, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderNetworkTarget(tt.probeType, tt.target, tt.host, tt.port)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RenderNetworkTarget() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("RenderNetworkTarget() = %q, want %q", got, tt.want)
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
		{name: "packet counts inverted", mutate: func(result *NetworkProbeResult) { result.SentPackets = 1; result.ReceivedPackets = 2 }},
		{name: "negative loss", mutate: func(result *NetworkProbeResult) { value := -1.0; result.LossPercent = &value }},
		{name: "loss over 100", mutate: func(result *NetworkProbeResult) { value := 101.0; result.LossPercent = &value }},
		{name: "negative jitter", mutate: func(result *NetworkProbeResult) { value := -1.0; result.JitterMS = &value }},
		{name: "negative minimum", mutate: func(result *NetworkProbeResult) { value := -1.0; result.MinLatencyMS = &value }},
		{name: "inverted latency range", mutate: func(result *NetworkProbeResult) {
			low, high := 2.0, 1.0
			result.MinLatencyMS = &low
			result.MaxLatencyMS = &high
		}},
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
