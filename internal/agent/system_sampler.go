package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"

	"github.com/beat/backend/internal/model"
)

type SystemSampler struct{}

var AgentVersion = "dev"

var countLogicalCPUs = func(ctx context.Context) (int, error) {
	return cpu.CountsWithContext(ctx, true)
}

var (
	readCPUPercent  = cpu.PercentWithContext
	readCPUInfo     = cpu.InfoWithContext
	readHostInfo    = host.InfoWithContext
	readLoadAverage = load.AvgWithContext
	readMemory      = mem.VirtualMemoryWithContext
	readSwapMemory  = mem.SwapMemoryWithContext
	readPartitions  = disk.PartitionsWithContext
	readUsage       = disk.UsageWithContext
	readDiskIO      = disk.IOCountersWithContext
	readNetworkIO   = gopsnet.IOCountersWithContext
	readConnections = gopsnet.ConnectionsWithContext
)

type ioCounters struct {
	disks    map[string]disk.IOCountersStat
	networks []gopsnet.IOCountersStat
}

type diskUsageStat struct {
	Total       uint64
	Used        uint64
	UsedPercent float64
}

type systemSnapshot struct {
	systemInfo model.SystemInfo
	cpuPercent float64
	cpuTotal   float64
	memory     mem.VirtualMemoryStat
	swap       mem.SwapMemoryStat
	load       load.AvgStat
	host       host.InfoStat
	diskUsage  diskUsageStat
	counters   ioCounters
	tcpCount   int
	udpCount   int
}

func (SystemSampler) Sample(ctx context.Context) (RawSample, error) {
	cpuPercent, cpuTotal, err := sampleCPU(ctx)
	if err != nil {
		return RawSample{}, err
	}
	systemInfo, hostInfo, err := sampleSystemInfo(ctx)
	if err != nil {
		return RawSample{}, err
	}
	memory, swap, loadAverage, diskStat, err := sampleResources(ctx)
	if err != nil {
		return RawSample{}, err
	}
	counters, err := sampleIOCounters(ctx)
	if err != nil {
		return RawSample{}, err
	}
	tcpCount, udpCount, err := sampleConnectionCounts(ctx)
	if err != nil {
		return RawSample{}, err
	}
	return aggregateSample(systemSnapshot{
		systemInfo: systemInfo,
		cpuPercent: cpuPercent,
		cpuTotal:   cpuTotal,
		memory:     memory,
		swap:       swap,
		load:       loadAverage,
		host:       hostInfo,
		diskUsage:  diskStat,
		counters:   counters,
		tcpCount:   tcpCount,
		udpCount:   udpCount,
	}), nil
}

func sampleCPU(ctx context.Context) (float64, float64, error) {
	percentages, err := readCPUPercent(ctx, 0, false)
	if err != nil {
		return 0, 0, fmt.Errorf("read cpu usage: %w", err)
	}
	if len(percentages) == 0 {
		return 0, 0, fmt.Errorf("read cpu usage: no values returned")
	}
	total, err := countLogicalCPUs(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read logical cpu count: %w", err)
	}
	if total <= 0 {
		return 0, 0, fmt.Errorf("read logical cpu count: no values returned")
	}
	return percentages[0], float64(total), nil
}

func sampleResources(ctx context.Context) (
	mem.VirtualMemoryStat, mem.SwapMemoryStat, load.AvgStat, diskUsageStat, error,
) {
	memory, err := readMemory(ctx)
	if err != nil {
		return mem.VirtualMemoryStat{}, mem.SwapMemoryStat{}, load.AvgStat{}, diskUsageStat{}, fmt.Errorf("read memory usage: %w", err)
	}
	swap, err := readSwapMemory(ctx)
	if err != nil {
		return mem.VirtualMemoryStat{}, mem.SwapMemoryStat{}, load.AvgStat{}, diskUsageStat{}, fmt.Errorf("read swap usage: %w", err)
	}
	average, err := readLoadAverage(ctx)
	if err != nil {
		return mem.VirtualMemoryStat{}, mem.SwapMemoryStat{}, load.AvgStat{}, diskUsageStat{}, fmt.Errorf("read load average: %w", err)
	}
	diskStat, err := sampleDiskUsage(ctx)
	if err != nil {
		return mem.VirtualMemoryStat{}, mem.SwapMemoryStat{}, load.AvgStat{}, diskUsageStat{}, fmt.Errorf("read disk usage: %w", err)
	}
	return *memory, *swap, *average, diskStat, nil
}

func sampleDiskUsage(ctx context.Context) (diskUsageStat, error) {
	partitions, err := readPartitions(ctx, false)
	if err != nil {
		return diskUsageStat{}, fmt.Errorf("read disk partitions: %w", err)
	}
	var results []mountUsage
	seenDevices := make(map[string]struct{}, len(partitions))
	for _, partition := range partitions {
		if partition.Mountpoint == "" || partition.Device == "" {
			continue
		}
		if _, duplicate := seenDevices[partition.Device]; duplicate {
			continue
		}
		seenDevices[partition.Device] = struct{}{}
		usage, err := readUsage(ctx, partition.Mountpoint)
		if err != nil {
			continue
		}
		results = append(results, mountUsage{mountpoint: partition.Mountpoint, usage: usage})
	}
	results = deduplicateByCapacity(results)
	var total uint64
	var used uint64
	for _, result := range results {
		total += result.usage.Total
		used += result.usage.Used
	}
	if total == 0 {
		return diskUsageStat{}, nil
	}
	return diskUsageStat{
		Total: total, Used: used, UsedPercent: float64(used) / float64(total) * 100,
	}, nil
}

type mountUsage struct {
	mountpoint string
	usage      *disk.UsageStat
}

func deduplicateByCapacity(results []mountUsage) []mountUsage {
	selected := make([]mountUsage, 0, len(results))
	for _, result := range results {
		overlap := false
		for _, existing := range selected {
			if existing.usage.Total != result.usage.Total {
				continue
			}
			if isSubpath(result.mountpoint, existing.mountpoint) || isSubpath(existing.mountpoint, result.mountpoint) {
				overlap = true
				break
			}
		}
		if !overlap {
			selected = append(selected, result)
		}
	}
	return selected
}

func isSubpath(candidate, parent string) bool {
	if candidate == parent {
		return true
	}
	if !strings.HasPrefix(candidate, parent) {
		return false
	}
	if strings.HasSuffix(parent, string(filepath.Separator)) {
		return true
	}
	return len(candidate) > len(parent) && candidate[len(parent)] == filepath.Separator
}

func sampleIOCounters(ctx context.Context) (ioCounters, error) {
	disks, err := readDiskIO(ctx)
	if err != nil {
		return ioCounters{}, fmt.Errorf("read disk counters: %w", err)
	}
	networks, err := readNetworkIO(ctx, false)
	if err != nil {
		return ioCounters{}, fmt.Errorf("read network counters: %w", err)
	}
	return ioCounters{disks: disks, networks: networks}, nil
}

func sampleConnectionCounts(ctx context.Context) (int, int, error) {
	tcpConnections, err := readConnections(ctx, "tcp")
	if err != nil {
		return 0, 0, fmt.Errorf("read tcp connections: %w", err)
	}
	udpConnections, err := readConnections(ctx, "udp")
	if err != nil {
		return 0, 0, fmt.Errorf("read udp connections: %w", err)
	}
	return len(tcpConnections), len(udpConnections), nil
}

func sampleSystemInfo(ctx context.Context) (model.SystemInfo, host.InfoStat, error) {
	hostInfo, err := readHostInfo(ctx)
	if err != nil {
		return model.SystemInfo{}, host.InfoStat{}, fmt.Errorf("read host information: %w", err)
	}
	cpuInfo, err := readCPUInfo(ctx)
	if err != nil {
		return model.SystemInfo{}, host.InfoStat{}, fmt.Errorf("read cpu information: %w", err)
	}
	cpuModel := ""
	if len(cpuInfo) > 0 {
		cpuModel = cpuInfo[0].ModelName
	}
	virtualization := strings.TrimSpace(strings.Join([]string{
		hostInfo.VirtualizationSystem, hostInfo.VirtualizationRole,
	}, " "))
	return model.SystemInfo{
		CPUModel: cpuModel, OS: hostInfo.OS, Platform: hostInfo.Platform,
		OSVersion: hostInfo.PlatformVersion, Kernel: hostInfo.KernelVersion,
		Arch: hostInfo.KernelArch, Virtualization: virtualization, AgentVersion: AgentVersion,
	}, *hostInfo, nil
}

func aggregateSample(snapshot systemSnapshot) RawSample {
	sample := RawSample{
		SystemInfo:     snapshot.systemInfo,
		CPU:            snapshot.cpuPercent,
		CPUUsed:        snapshot.cpuPercent * snapshot.cpuTotal / 100,
		CPUTotal:       snapshot.cpuTotal,
		Memory:         snapshot.memory.UsedPercent,
		MemoryUsed:     snapshot.memory.Used,
		MemoryTotal:    snapshot.memory.Total,
		Disk:           snapshot.diskUsage.UsedPercent,
		DiskUsed:       snapshot.diskUsage.Used,
		DiskTotal:      snapshot.diskUsage.Total,
		SwapUsed:       snapshot.swap.Used,
		SwapTotal:      snapshot.swap.Total,
		SwapPercent:    snapshot.swap.UsedPercent,
		Load1:          snapshot.load.Load1,
		Load5:          snapshot.load.Load5,
		Load15:         snapshot.load.Load15,
		Uptime:         snapshot.host.Uptime,
		Processes:      int(snapshot.host.Procs),
		TCPConnections: snapshot.tcpCount,
		UDPConnections: snapshot.udpCount,
	}
	for _, counter := range snapshot.counters.disks {
		sample.DiskRead += counter.ReadBytes
		sample.DiskWrite += counter.WriteBytes
	}
	for _, counter := range snapshot.counters.networks {
		sample.NetRecv += counter.BytesRecv
		sample.NetSent += counter.BytesSent
	}
	return sample
}
