package model

import (
	"fmt"
	"math"
	"time"
)

type NodeMetrics struct {
	SystemInfo     SystemInfo `json:"-"`
	CPU            float64    `json:"cpu"`
	CPUUsed        float64    `json:"cpu_used"`
	CPUTotal       float64    `json:"cpu_total"`
	Memory         float64    `json:"memory"`
	MemoryUsed     float64    `json:"memory_used"`
	MemoryTotal    float64    `json:"memory_total"`
	Disk           float64    `json:"disk"`
	DiskUsed       float64    `json:"disk_used"`
	DiskTotal      float64    `json:"disk_total"`
	DiskRead       float64    `json:"disk_read"`
	DiskWrite      float64    `json:"disk_write"`
	NetRecv        float64    `json:"net_recv"`
	NetSent        float64    `json:"net_sent"`
	NetRecvTotal   float64    `json:"net_recv_total"`
	NetSentTotal   float64    `json:"net_sent_total"`
	Swap           float64    `json:"swap"`
	SwapUsed       float64    `json:"swap_used"`
	SwapTotal      float64    `json:"swap_total"`
	Load1          float64    `json:"load1"`
	Load5          float64    `json:"load5"`
	Load15         float64    `json:"load15"`
	Uptime         float64    `json:"uptime"`
	Processes      float64    `json:"processes"`
	TCPConnections float64    `json:"tcp_connections"`
	UDPConnections float64    `json:"udp_connections"`
}

type MetricValue struct {
	Name  string
	Value float64
}

func (metrics NodeMetrics) TimeSeries() []MetricValue {
	return []MetricValue{
		{Name: "cpu", Value: metrics.CPU},
		{Name: "cpu_used", Value: metrics.CPUUsed},
		{Name: "cpu_total", Value: metrics.CPUTotal},
		{Name: "memory", Value: metrics.Memory},
		{Name: "memory_used", Value: metrics.MemoryUsed},
		{Name: "memory_total", Value: metrics.MemoryTotal},
		{Name: "disk", Value: metrics.Disk},
		{Name: "disk_used", Value: metrics.DiskUsed},
		{Name: "disk_total", Value: metrics.DiskTotal},
		{Name: "disk_read", Value: metrics.DiskRead},
		{Name: "disk_write", Value: metrics.DiskWrite},
		{Name: "net_recv", Value: metrics.NetRecv},
		{Name: "net_sent", Value: metrics.NetSent},
		{Name: "net_recv_total", Value: metrics.NetRecvTotal},
		{Name: "net_sent_total", Value: metrics.NetSentTotal},
		{Name: "swap", Value: metrics.Swap},
		{Name: "swap_used", Value: metrics.SwapUsed},
		{Name: "swap_total", Value: metrics.SwapTotal},
		{Name: "load1", Value: metrics.Load1},
		{Name: "load5", Value: metrics.Load5},
		{Name: "load15", Value: metrics.Load15},
		{Name: "uptime", Value: metrics.Uptime},
		{Name: "processes", Value: metrics.Processes},
		{Name: "tcp_connections", Value: metrics.TCPConnections},
		{Name: "udp_connections", Value: metrics.UDPConnections},
	}
}

func MetricNames() []string {
	metrics := (NodeMetrics{}).TimeSeries()
	names := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		names = append(names, metric.Name)
	}
	return names
}

type MetricData struct {
	NodeID    string    `json:"node_id"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

func (metrics NodeMetrics) Validate() error {
	percentages := []struct {
		name  string
		value float64
	}{
		{"cpu", metrics.CPU}, {"memory", metrics.Memory},
		{"disk", metrics.Disk}, {"swap", metrics.Swap},
	}
	for _, metric := range percentages {
		if !isFinite(metric.value) || metric.value < 0 || metric.value > 100 {
			return fmt.Errorf("%s percentage must be between 0 and 100", metric.name)
		}
	}
	values := []struct {
		name  string
		value float64
	}{
		{"cpu_used", metrics.CPUUsed}, {"cpu_total", metrics.CPUTotal},
		{"memory_used", metrics.MemoryUsed}, {"memory_total", metrics.MemoryTotal},
		{"disk_used", metrics.DiskUsed}, {"disk_total", metrics.DiskTotal},
		{"disk_read", metrics.DiskRead}, {"disk_write", metrics.DiskWrite},
		{"net_recv", metrics.NetRecv}, {"net_sent", metrics.NetSent},
		{"net_recv_total", metrics.NetRecvTotal}, {"net_sent_total", metrics.NetSentTotal},
		{"swap_used", metrics.SwapUsed}, {"swap_total", metrics.SwapTotal},
		{"load1", metrics.Load1}, {"load5", metrics.Load5}, {"load15", metrics.Load15},
		{"uptime", metrics.Uptime}, {"processes", metrics.Processes},
		{"tcp_connections", metrics.TCPConnections}, {"udp_connections", metrics.UDPConnections},
	}
	for _, metric := range values {
		if !isFinite(metric.value) || metric.value < 0 {
			return fmt.Errorf("%s must be a finite non-negative number", metric.name)
		}
	}
	capacities := []struct {
		name        string
		used, total float64
	}{
		{"cpu", metrics.CPUUsed, metrics.CPUTotal},
		{"memory", metrics.MemoryUsed, metrics.MemoryTotal},
		{"disk", metrics.DiskUsed, metrics.DiskTotal},
		{"swap", metrics.SwapUsed, metrics.SwapTotal},
	}
	for _, capacity := range capacities {
		if capacity.total > 0 && capacity.used > capacity.total {
			return fmt.Errorf("%s used value must not exceed total", capacity.name)
		}
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
