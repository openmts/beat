package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"

	"github.com/beat/backend/internal/model"
)

func TestAggregateSample(t *testing.T) {
	sample := aggregateSample(systemSnapshot{
		systemInfo: model.SystemInfo{OS: "linux", AgentVersion: "test"},
		cpuPercent: 12.5,
		cpuTotal:   8,
		memory:     mem.VirtualMemoryStat{Used: 120, Total: 300, UsedPercent: 40},
		swap:       mem.SwapMemoryStat{Used: 25, Total: 100, UsedPercent: 25},
		load:       load.AvgStat{Load1: 1, Load5: 2, Load15: 3},
		host:       host.InfoStat{Uptime: 600, Procs: 20},
		rootUsage:  disk.UsageStat{Used: 80, Total: 200, UsedPercent: 40},
		counters: ioCounters{
			disks: map[string]disk.IOCountersStat{
				"a": {ReadBytes: 10, WriteBytes: 20},
				"b": {ReadBytes: 30, WriteBytes: 40},
			},
			networks: []gopsnet.IOCountersStat{
				{BytesRecv: 50, BytesSent: 60},
				{BytesRecv: 70, BytesSent: 80},
			},
		},
		tcpCount: 9,
		udpCount: 4,
	})
	if sample.CPU != 12.5 || sample.CPUUsed != 1 || sample.CPUTotal != 8 ||
		sample.Memory != 40 || sample.MemoryUsed != 120 || sample.MemoryTotal != 300 ||
		sample.DiskRead != 40 ||
		sample.DiskWrite != 60 || sample.NetRecv != 120 || sample.NetSent != 140 ||
		sample.Disk != 40 || sample.DiskUsed != 80 || sample.DiskTotal != 200 {
		t.Fatalf("sample = %#v", sample)
	}
	if sample.SystemInfo.OS != "linux" || sample.SystemInfo.AgentVersion != "test" ||
		sample.SwapUsed != 25 || sample.SwapTotal != 100 || sample.SwapPercent != 25 ||
		sample.Load1 != 1 || sample.Load5 != 2 || sample.Load15 != 3 ||
		sample.Uptime != 600 || sample.Processes != 20 ||
		sample.TCPConnections != 9 || sample.UDPConnections != 4 {
		t.Fatalf("extended sample = %#v", sample)
	}
}

func TestSystemSampler(t *testing.T) {
	sample, err := (SystemSampler{}).Sample(context.Background())
	if err != nil {
		t.Fatalf("sample system: %v", err)
	}
	if sample.CPU < 0 || sample.CPUUsed < 0 || sample.CPUTotal == 0 || sample.CPUUsed > float64(sample.CPUTotal) ||
		sample.Memory < 0 || sample.MemoryTotal == 0 || sample.MemoryUsed > sample.MemoryTotal ||
		sample.DiskTotal == 0 || sample.DiskUsed > sample.DiskTotal {
		t.Fatalf("sample = %#v", sample)
	}
}

func TestSystemSamplerLogicalCPUCountErrors(t *testing.T) {
	original := countLogicalCPUs
	t.Cleanup(func() { countLogicalCPUs = original })

	countLogicalCPUs = func(context.Context) (int, error) {
		return 0, errors.New("count failed")
	}
	if _, err := (SystemSampler{}).Sample(context.Background()); err == nil {
		t.Fatal("expected logical CPU count error")
	}

	countLogicalCPUs = func(context.Context) (int, error) { return 0, nil }
	if _, err := (SystemSampler{}).Sample(context.Background()); err == nil {
		t.Fatal("expected empty logical CPU count error")
	}
}

func TestSampleCPUErrors(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readCPUPercent = func(context.Context, time.Duration, bool) ([]float64, error) {
		return nil, errors.New("cpu failed")
	}
	if _, _, err := sampleCPU(context.Background()); err == nil {
		t.Fatal("expected CPU read error")
	}
	readCPUPercent = func(context.Context, time.Duration, bool) ([]float64, error) { return []float64{}, nil }
	if _, _, err := sampleCPU(context.Background()); err == nil {
		t.Fatal("expected empty CPU values error")
	}
}

func TestSampleSystemInfoErrors(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readHostInfo = func(context.Context) (*host.InfoStat, error) { return nil, errors.New("host failed") }
	if _, _, err := sampleSystemInfo(context.Background()); err == nil {
		t.Fatal("expected host information error")
	}
	readHostInfo = func(context.Context) (*host.InfoStat, error) { return &host.InfoStat{}, nil }
	readCPUInfo = func(context.Context) ([]cpu.InfoStat, error) { return nil, errors.New("cpu info failed") }
	if _, _, err := sampleSystemInfo(context.Background()); err == nil {
		t.Fatal("expected CPU information error")
	}
}

func TestSampleResourceErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{"memory", func() {
			readMemory = func(context.Context) (*mem.VirtualMemoryStat, error) { return nil, errors.New("failed") }
		}},
		{"swap", func() {
			readSwapMemory = func(context.Context) (*mem.SwapMemoryStat, error) { return nil, errors.New("failed") }
		}},
		{"load", func() {
			readLoadAverage = func(context.Context) (*load.AvgStat, error) { return nil, errors.New("failed") }
		}},
		{"root", func() {
			readRootUsage = func(context.Context, string) (*disk.UsageStat, error) { return nil, errors.New("failed") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := preserveSystemReaders()
			t.Cleanup(restore)
			setSuccessfulResourceReaders()
			test.setup()
			if _, _, _, _, err := sampleResources(context.Background()); err == nil {
				t.Fatal("expected resource sampling error")
			}
		})
	}
}

func TestSampleIOAndConnectionErrors(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readDiskIO = func(context.Context, ...string) (map[string]disk.IOCountersStat, error) {
		return nil, errors.New("failed")
	}
	if _, err := sampleIOCounters(context.Background()); err == nil {
		t.Fatal("expected disk counter error")
	}
	readDiskIO = func(context.Context, ...string) (map[string]disk.IOCountersStat, error) {
		return map[string]disk.IOCountersStat{}, nil
	}
	readNetworkIO = func(context.Context, bool) ([]gopsnet.IOCountersStat, error) { return nil, errors.New("failed") }
	if _, err := sampleIOCounters(context.Background()); err == nil {
		t.Fatal("expected network counter error")
	}
	readConnections = func(context.Context, string) ([]gopsnet.ConnectionStat, error) { return nil, errors.New("failed") }
	if _, _, err := sampleConnectionCounts(context.Background()); err == nil {
		t.Fatal("expected TCP connection error")
	}
	readConnections = func(_ context.Context, kind string) ([]gopsnet.ConnectionStat, error) {
		if kind == "udp" {
			return nil, errors.New("failed")
		}
		return []gopsnet.ConnectionStat{}, nil
	}
	if _, _, err := sampleConnectionCounts(context.Background()); err == nil {
		t.Fatal("expected UDP connection error")
	}
}

func setSuccessfulResourceReaders() {
	readMemory = func(context.Context) (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil }
	readSwapMemory = func(context.Context) (*mem.SwapMemoryStat, error) { return &mem.SwapMemoryStat{}, nil }
	readLoadAverage = func(context.Context) (*load.AvgStat, error) { return &load.AvgStat{}, nil }
	readRootUsage = func(context.Context, string) (*disk.UsageStat, error) { return &disk.UsageStat{}, nil }
}

func preserveSystemReaders() func() {
	originalCPUPercent, originalCPUInfo := readCPUPercent, readCPUInfo
	originalHost, originalLoad := readHostInfo, readLoadAverage
	originalMemory, originalSwap := readMemory, readSwapMemory
	originalRoot, originalDisk := readRootUsage, readDiskIO
	originalNetwork, originalConnections := readNetworkIO, readConnections
	return func() {
		readCPUPercent, readCPUInfo = originalCPUPercent, originalCPUInfo
		readHostInfo, readLoadAverage = originalHost, originalLoad
		readMemory, readSwapMemory = originalMemory, originalSwap
		readRootUsage, readDiskIO = originalRoot, originalDisk
		readNetworkIO, readConnections = originalNetwork, originalConnections
	}
}
