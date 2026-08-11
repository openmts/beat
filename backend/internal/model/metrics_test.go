package model

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNodeMetricsTimeSeries(t *testing.T) {
	metrics := NodeMetrics{
		CPU: 1, CPUUsed: 2, CPUTotal: 3,
		Memory: 4, MemoryUsed: 5, MemoryTotal: 6,
		Disk: 7, DiskUsed: 8, DiskTotal: 9,
		DiskRead: 10, DiskWrite: 11,
		NetRecv: 12, NetSent: 13, NetRecvTotal: 14, NetSentTotal: 15,
		Swap: 16, SwapUsed: 17, SwapTotal: 18,
		Load1: 19, Load5: 20, Load15: 21,
		Uptime: 22, Processes: 23, TCPConnections: 24, UDPConnections: 25,
	}
	want := []MetricValue{
		{Name: "cpu", Value: 1}, {Name: "cpu_used", Value: 2}, {Name: "cpu_total", Value: 3},
		{Name: "memory", Value: 4}, {Name: "memory_used", Value: 5}, {Name: "memory_total", Value: 6},
		{Name: "disk", Value: 7}, {Name: "disk_used", Value: 8}, {Name: "disk_total", Value: 9},
		{Name: "disk_read", Value: 10}, {Name: "disk_write", Value: 11},
		{Name: "net_recv", Value: 12}, {Name: "net_sent", Value: 13},
		{Name: "net_recv_total", Value: 14}, {Name: "net_sent_total", Value: 15},
		{Name: "swap", Value: 16}, {Name: "swap_used", Value: 17}, {Name: "swap_total", Value: 18},
		{Name: "load1", Value: 19}, {Name: "load5", Value: 20}, {Name: "load15", Value: 21},
		{Name: "uptime", Value: 22}, {Name: "processes", Value: 23},
		{Name: "tcp_connections", Value: 24}, {Name: "udp_connections", Value: 25},
	}

	if got := metrics.TimeSeries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("TimeSeries() = %#v, want %#v", got, want)
	}
	wantNames := make([]string, 0, len(want))
	for _, metric := range want {
		wantNames = append(wantNames, metric.Name)
	}
	if got := MetricNames(); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("MetricNames() = %#v, want %#v", got, wantNames)
	}
}

func TestMetricNamesCoverEveryReportedNumericField(t *testing.T) {
	metricType := reflect.TypeOf(NodeMetrics{})
	want := make(map[string]struct{}, metricType.NumField())
	for index := range metricType.NumField() {
		field := metricType.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if field.Type.Kind() != reflect.Float64 {
			t.Fatalf("reported metric field %s must be float64", field.Name)
		}
		want[name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(want))
	for _, name := range MetricNames() {
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("MetricNames() contains duplicate %q", name)
		}
		if _, reported := want[name]; !reported {
			t.Fatalf("MTS metric %q has no reported numeric field", name)
		}
		seen[name] = struct{}{}
		delete(want, name)
	}
	if len(want) == 0 {
		return
	}

	missing := make([]string, 0, len(want))
	for name := range want {
		missing = append(missing, name)
	}
	slices.Sort(missing)
	t.Fatalf("reported metrics missing from MTS mapping: %v", missing)
}

func TestNodeMetricsValidate(t *testing.T) {
	valid := NodeMetrics{
		CPU: 50, CPUUsed: 4, CPUTotal: 8,
		Memory: 50, MemoryUsed: 4, MemoryTotal: 8,
		Disk: 50, DiskUsed: 4, DiskTotal: 8,
		Swap: 50, SwapUsed: 4, SwapTotal: 8,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("validate metrics: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*NodeMetrics)
	}{
		{"percentage", func(metrics *NodeMetrics) { metrics.CPU = 101 }},
		{"negative", func(metrics *NodeMetrics) { metrics.Load1 = -1 }},
		{"not finite", func(metrics *NodeMetrics) { metrics.NetRecv = math.Inf(1) }},
		{"used exceeds total", func(metrics *NodeMetrics) { metrics.MemoryUsed = 9 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics := valid
			test.mutate(&metrics)
			if err := metrics.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
