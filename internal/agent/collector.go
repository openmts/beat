package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

type RawSample struct {
	SystemInfo     model.SystemInfo
	CPU            float64
	CPUUsed        float64
	CPUTotal       float64
	Memory         float64
	MemoryUsed     uint64
	MemoryTotal    uint64
	Disk           float64
	DiskUsed       uint64
	DiskTotal      uint64
	DiskRead       uint64
	DiskWrite      uint64
	NetRecv        uint64
	NetSent        uint64
	SwapUsed       uint64
	SwapTotal      uint64
	SwapPercent    float64
	Load1          float64
	Load5          float64
	Load15         float64
	Uptime         uint64
	Processes      int
	TCPConnections int
	UDPConnections int
}

type Sampler interface {
	Sample(context.Context) (RawSample, error)
}

type Collector struct {
	sampler Sampler
	now     func() time.Time
	mu      sync.Mutex
	last    RawSample
	lastAt  time.Time
}

func NewCollector(sampler Sampler, now func() time.Time) *Collector {
	return &Collector{sampler: sampler, now: now}
}

func (c *Collector) Collect(ctx context.Context) (model.NodeMetrics, error) {
	sample, err := c.sampler.Sample(ctx)
	if err != nil {
		return model.NodeMetrics{}, fmt.Errorf("sample system metrics: %w", err)
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	metrics := model.NodeMetrics{
		SystemInfo:   sample.SystemInfo,
		CPU:          sample.CPU,
		CPUUsed:      sample.CPUUsed,
		CPUTotal:     sample.CPUTotal,
		Memory:       sample.Memory,
		MemoryUsed:   float64(sample.MemoryUsed),
		MemoryTotal:  float64(sample.MemoryTotal),
		Disk:         sample.Disk,
		DiskUsed:     float64(sample.DiskUsed),
		DiskTotal:    float64(sample.DiskTotal),
		NetRecvTotal: float64(sample.NetRecv),
		NetSentTotal: float64(sample.NetSent),
		Swap:         sample.SwapPercent, SwapUsed: float64(sample.SwapUsed), SwapTotal: float64(sample.SwapTotal),
		Load1: sample.Load1, Load5: sample.Load5, Load15: sample.Load15,
		Uptime: float64(sample.Uptime), Processes: float64(sample.Processes),
		TCPConnections: float64(sample.TCPConnections), UDPConnections: float64(sample.UDPConnections),
	}
	if !c.lastAt.IsZero() {
		seconds := now.Sub(c.lastAt).Seconds()
		metrics.DiskRead = counterRate(sample.DiskRead, c.last.DiskRead, seconds)
		metrics.DiskWrite = counterRate(sample.DiskWrite, c.last.DiskWrite, seconds)
		metrics.NetRecv = counterRate(sample.NetRecv, c.last.NetRecv, seconds)
		metrics.NetSent = counterRate(sample.NetSent, c.last.NetSent, seconds)
	}
	c.last = sample
	c.lastAt = now
	return metrics, nil
}

func counterRate(current, previous uint64, seconds float64) float64 {
	if current < previous || seconds <= 0 {
		return 0
	}
	return float64(current-previous) / seconds
}
