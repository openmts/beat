package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeSampler struct {
	samples []RawSample
	err     error
	index   int
}

func (f *fakeSampler) Sample(context.Context) (RawSample, error) {
	if f.err != nil {
		return RawSample{}, f.err
	}
	sample := f.samples[f.index]
	if f.index < len(f.samples)-1 {
		f.index++
	}
	return sample, nil
}

func TestCollectorCalculatesRates(t *testing.T) {
	now := time.Unix(100, 0)
	sampler := &fakeSampler{samples: []RawSample{
		{
			SystemInfo: model.SystemInfo{OS: "linux", AgentVersion: "test"},
			CPU:        25, CPUUsed: 2, CPUTotal: 8,
			Memory: 50, MemoryUsed: 400, MemoryTotal: 800,
			Disk: 40, DiskUsed: 40, DiskTotal: 100,
			DiskRead: 100, DiskWrite: 200, NetRecv: 300, NetSent: 400,
			SwapUsed: 10, SwapTotal: 100, SwapPercent: 10,
			Load1: 1, Load5: 2, Load15: 3, Uptime: 3600, Processes: 42,
			TCPConnections: 7, UDPConnections: 3,
		},
		{
			CPU: 30, CPUUsed: 2.4, CPUTotal: 8,
			Memory: 60, MemoryUsed: 480, MemoryTotal: 800,
			Disk: 45, DiskUsed: 45, DiskTotal: 100,
			DiskRead: 300, DiskWrite: 500, NetRecv: 700, NetSent: 1000,
		},
	}}
	collector := NewCollector(sampler, func() time.Time { return now })
	first, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if first.DiskRead != 0 || first.NetRecv != 0 {
		t.Fatalf("first rates = %#v, want zero", first)
	}
	if first.DiskUsed != 40 || first.DiskTotal != 100 {
		t.Fatalf("first disk usage = %#v", first)
	}
	if first.CPUUsed != 2 || first.CPUTotal != 8 || first.MemoryUsed != 400 || first.MemoryTotal != 800 {
		t.Fatalf("first resource usage = %#v", first)
	}
	if first.SystemInfo.OS != "linux" || first.SystemInfo.AgentVersion != "test" ||
		first.NetRecvTotal != 300 || first.NetSentTotal != 400 ||
		first.Swap != 10 || first.SwapUsed != 10 || first.SwapTotal != 100 ||
		first.Load1 != 1 || first.Load5 != 2 || first.Load15 != 3 || first.Uptime != 3600 ||
		first.Processes != 42 || first.TCPConnections != 7 || first.UDPConnections != 3 {
		t.Fatalf("first extended metrics = %#v", first)
	}

	now = now.Add(2 * time.Second)
	second, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if second.DiskRead != 100 || second.DiskWrite != 150 || second.NetRecv != 200 || second.NetSent != 300 {
		t.Fatalf("second rates = %#v", second)
	}
	if second.CPU != 30 || second.CPUUsed != 2.4 || second.CPUTotal != 8 ||
		second.Memory != 60 || second.MemoryUsed != 480 || second.MemoryTotal != 800 ||
		second.Disk != 45 || second.DiskUsed != 45 || second.DiskTotal != 100 {
		t.Fatalf("usage values = %#v", second)
	}
}

func TestCollectorErrorsAndCounterReset(t *testing.T) {
	collector := NewCollector(&fakeSampler{err: errors.New("sample failed")}, time.Now)
	if _, err := collector.Collect(context.Background()); err == nil {
		t.Fatal("expected sample error")
	}

	now := time.Unix(100, 0)
	collector = NewCollector(&fakeSampler{samples: []RawSample{{DiskRead: 100}, {DiskRead: 10}}}, func() time.Time { return now })
	_, _ = collector.Collect(context.Background())
	now = now.Add(time.Second)
	metrics, err := collector.Collect(context.Background())
	if err != nil || metrics.DiskRead != 0 {
		t.Fatalf("metrics = %#v, error = %v", metrics, err)
	}
}
