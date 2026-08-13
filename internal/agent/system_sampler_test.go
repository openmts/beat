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
		diskUsage:  diskUsageStat{Used: 80, Total: 200, UsedPercent: 40},
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

func TestSystemSamplerPropagatesEachStepError(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{"cpu", func() {
			readCPUPercent = func(context.Context, time.Duration, bool) ([]float64, error) { return nil, errors.New("cpu") }
		}},
		{"system info", func() {
			readHostInfo = func(context.Context) (*host.InfoStat, error) { return nil, errors.New("host") }
		}},
		{"resources", func() {
			readMemory = func(context.Context) (*mem.VirtualMemoryStat, error) { return nil, errors.New("memory") }
		}},
		{"io counters", func() {
			readDiskIO = func(context.Context, ...string) (map[string]disk.IOCountersStat, error) {
				return nil, errors.New("disk")
			}
		}},
		{"connections", func() {
			readConnections = func(context.Context, string) ([]gopsnet.ConnectionStat, error) { return nil, errors.New("tcp") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := preserveSystemReaders()
			t.Cleanup(restore)
			test.setup()
			if _, err := (SystemSampler{}).Sample(context.Background()); err == nil {
				t.Fatal("expected sampling error propagation")
			}
		})
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
			readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) { return nil, errors.New("failed") }
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

func TestSampleDiskUsage(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
			{Device: "/dev/sda1", Mountpoint: "/mnt/bind", Fstype: "ext4"},
			{Device: "/dev/sdb1", Mountpoint: "/data", Fstype: "ext4"},
			{Device: "", Mountpoint: "/proc", Fstype: "proc"},
			{Device: "/dev/sdc1", Mountpoint: "", Fstype: "ext4"},
			{Device: "/dev/sdd1", Mountpoint: "/unreadable", Fstype: "ext4"},
		}, nil
	}
	readUsage = func(_ context.Context, mountpoint string) (*disk.UsageStat, error) {
		switch mountpoint {
		case "/":
			return &disk.UsageStat{Total: 100, Used: 40, UsedPercent: 40}, nil
		case "/mnt/bind":
			return &disk.UsageStat{Total: 100, Used: 40, UsedPercent: 40}, nil
		case "/data":
			return &disk.UsageStat{Total: 200, Used: 60, UsedPercent: 30}, nil
		}
		return nil, errors.New("usage failed")
	}
	got, err := sampleDiskUsage(context.Background())
	if err != nil {
		t.Fatalf("sample disk usage: %v", err)
	}
	if got.Total != 300 || got.Used != 100 {
		t.Fatalf("disk usage = %+v, want total 300 used 100", got)
	}
	if got.UsedPercent != float64(100)/float64(300)*100 {
		t.Fatalf("used percent = %v, want %v", got.UsedPercent, float64(100)/float64(300)*100)
	}
}

func TestSampleDiskUsageEmptyAndPartitionFailure(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) { return nil, nil }
	got, err := sampleDiskUsage(context.Background())
	if err != nil || got.Total != 0 || got.Used != 0 {
		t.Fatalf("empty partitions = %+v, %v", got, err)
	}
	readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) { return nil, errors.New("failed") }
	if _, err := sampleDiskUsage(context.Background()); err == nil {
		t.Fatal("expected partition error")
	}
}

func TestSampleDiskUsageMacOSAPFS(t *testing.T) {
	restore := preserveSystemReaders()
	t.Cleanup(restore)
	readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Device: "/dev/disk3s1", Mountpoint: "/", Fstype: "apfs"},
			{Device: "/dev/disk3s5", Mountpoint: "/System/Volumes/Data", Fstype: "apfs"},
			{Device: "/dev/disk4s1", Mountpoint: "/Volumes/External", Fstype: "apfs"},
		}, nil
	}
	readUsage = func(_ context.Context, mountpoint string) (*disk.UsageStat, error) {
		switch mountpoint {
		case "/":
			return &disk.UsageStat{Total: 1000, Used: 400, UsedPercent: 40}, nil
		case "/System/Volumes/Data":
			return &disk.UsageStat{Total: 1000, Used: 400, UsedPercent: 40}, nil
		case "/Volumes/External":
			return &disk.UsageStat{Total: 2000, Used: 800, UsedPercent: 40}, nil
		}
		return nil, errors.New("usage failed")
	}
	got, err := sampleDiskUsage(context.Background())
	if err != nil {
		t.Fatalf("sample disk usage: %v", err)
	}
	if got.Total != 3000 || got.Used != 1200 {
		t.Fatalf("disk usage = %+v, want total 3000 used 1200", got)
	}
}

func TestDeduplicateByCapacity(t *testing.T) {
	usage := func(total uint64, used uint64) *disk.UsageStat {
		return &disk.UsageStat{Total: total, Used: used}
	}
	tests := []struct {
		name    string
		results []mountUsage
		want    []mountUsage
	}{
		{"non overlapping", []mountUsage{
			{mountpoint: "/", usage: usage(100, 40)},
			{mountpoint: "/data", usage: usage(200, 60)},
		}, []mountUsage{
			{mountpoint: "/", usage: usage(100, 40)},
			{mountpoint: "/data", usage: usage(200, 60)},
		}},
		{"apfs system and data volume", []mountUsage{
			{mountpoint: "/", usage: usage(1000, 400)},
			{mountpoint: "/System/Volumes/Data", usage: usage(1000, 400)},
		}, []mountUsage{
			{mountpoint: "/", usage: usage(1000, 400)},
		}},
		{"same total distinct paths", []mountUsage{
			{mountpoint: "/a", usage: usage(1000, 400)},
			{mountpoint: "/b", usage: usage(1000, 400)},
		}, []mountUsage{
			{mountpoint: "/a", usage: usage(1000, 400)},
			{mountpoint: "/b", usage: usage(1000, 400)},
		}},
		{"child and parent equal total", []mountUsage{
			{mountpoint: "/mnt", usage: usage(500, 100)},
			{mountpoint: "/mnt/sub", usage: usage(500, 100)},
		}, []mountUsage{
			{mountpoint: "/mnt", usage: usage(500, 100)},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := deduplicateByCapacity(test.results)
			if len(got) != len(test.want) {
				t.Fatalf("got %d entries, want %d: %+v", len(got), len(test.want), got)
			}
			for i := range test.want {
				if got[i].mountpoint != test.want[i].mountpoint || got[i].usage.Total != test.want[i].usage.Total {
					t.Fatalf("entry %d = %+v, want %+v", i, got[i], test.want[i])
				}
			}
		})
	}
}

func TestIsSubpath(t *testing.T) {
	tests := []struct {
		candidate string
		parent    string
		want      bool
	}{
		{"/", "/", true},
		{"/System/Volumes/Data", "/", true},
		{"/", "/System/Volumes/Data", false},
		{"/data", "/data", true},
		{"/data/sub", "/data", true},
		{"/data2", "/data", false},
		{"/data/sub", "/data/sub", true},
		{"/mnt", "/mnt/sub", false},
	}
	for _, test := range tests {
		if got := isSubpath(test.candidate, test.parent); got != test.want {
			t.Fatalf("isSubpath(%q, %q) = %v, want %v", test.candidate, test.parent, got, test.want)
		}
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
	readPartitions = func(context.Context, bool) ([]disk.PartitionStat, error) { return nil, nil }
	readUsage = func(context.Context, string) (*disk.UsageStat, error) { return &disk.UsageStat{}, nil }
}

func preserveSystemReaders() func() {
	originalCPUPercent, originalCPUInfo := readCPUPercent, readCPUInfo
	originalHost, originalLoad := readHostInfo, readLoadAverage
	originalMemory, originalSwap := readMemory, readSwapMemory
	originalPartitions, originalUsage := readPartitions, readUsage
	originalDisk, originalNetwork := readDiskIO, readNetworkIO
	originalConnections := readConnections
	return func() {
		readCPUPercent, readCPUInfo = originalCPUPercent, originalCPUInfo
		readHostInfo, readLoadAverage = originalHost, originalLoad
		readMemory, readSwapMemory = originalMemory, originalSwap
		readPartitions, readUsage = originalPartitions, originalUsage
		readDiskIO, readNetworkIO = originalDisk, originalNetwork
		readConnections = originalConnections
	}
}
