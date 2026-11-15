package agent

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"beat/internal/domain"
)

type Snapshot struct {
	Hostname   string
	Address    string
	OS         string
	Kernel     string
	Arch       string
	CPUCores   int
	CPUModel   string
	Metrics    domain.Metrics
	Interfaces []domain.NetworkInterface
}

type Collector struct {
	diskPath string
	lastCPU  cpuTimes
	lastNet  netCounters
	lastAt   time.Time
}

type cpuTimes struct {
	total  uint64
	idle   uint64
	user   uint64
	system uint64
	iowait uint64
}

type netCounters struct {
	rxBytes uint64
	txBytes uint64
}

func NewCollector(diskPath string) *Collector {
	if strings.TrimSpace(diskPath) == "" {
		diskPath = "/"
	}
	return &Collector{diskPath: diskPath}
}

func (c *Collector) Collect() (Snapshot, error) {
	now := time.Now().UTC()
	hostname, _ := os.Hostname()
	cpu, err := readCPU()
	if err != nil {
		return Snapshot{}, err
	}
	memory, err := readMemory()
	if err != nil {
		return Snapshot{}, err
	}
	disk, err := readDisk(c.diskPath)
	if err != nil {
		return Snapshot{}, err
	}
	network, interfaces, err := readNetwork()
	if err != nil {
		return Snapshot{}, err
	}
	load1, load5, load15 := readLoad()
	uptime := readUptime()
	processes := countProcesses()
	tcp := countTCP()

	metrics := domain.Metrics{
		Timestamp: now, MemoryTotalBytes: memory.total, MemoryUsedBytes: memory.used,
		MemoryUsage: percent(memory.used, memory.total), SwapUsedBytes: memory.swapUsed,
		DiskTotalBytes: disk.total, DiskUsedBytes: disk.used, DiskUsage: percent(disk.used, disk.total),
		NetworkRxBps: 0, NetworkTxBps: 0, Load1: load1, Load5: load5, Load15: load15,
		Processes: processes, TCPConnections: tcp, UptimeSeconds: uptime,
	}
	if c.lastCPU.total > 0 {
		totalDelta := cpu.total - c.lastCPU.total
		if totalDelta > 0 {
			busy := totalDelta - (cpu.idle - c.lastCPU.idle)
			metrics.CPUUsage = round(float64(busy)/float64(totalDelta)*100, 2)
			metrics.CPUUser = round(float64(cpu.user-c.lastCPU.user)/float64(totalDelta)*100, 2)
			metrics.CPUSystem = round(float64(cpu.system-c.lastCPU.system)/float64(totalDelta)*100, 2)
			metrics.CPUIowait = round(float64(cpu.iowait-c.lastCPU.iowait)/float64(totalDelta)*100, 2)
		}
	} else if cpu.total > 0 {
		metrics.CPUUsage = round(float64(cpu.total-cpu.idle)/float64(cpu.total)*100, 2)
	}
	if !c.lastAt.IsZero() {
		elapsed := now.Sub(c.lastAt).Seconds()
		if elapsed > 0 {
			metrics.NetworkRxBps = round(float64(network.rxBytes-c.lastNet.rxBytes)/elapsed, 0)
			metrics.NetworkTxBps = round(float64(network.txBytes-c.lastNet.txBytes)/elapsed, 0)
		}
	}
	c.lastCPU = cpu
	c.lastNet = network
	c.lastAt = now

	osName := readOSName()
	kernel := readKernel()
	model := readCPUModel()
	return Snapshot{
		Hostname: hostname, Address: localAddress(), OS: osName, Kernel: kernel, Arch: runtime.GOARCH,
		CPUCores: runtime.NumCPU(), CPUModel: model, Metrics: metrics, Interfaces: interfaces,
	}, nil
}

func readCPU() (cpuTimes, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 || fields[0] != "cpu" {
			continue
		}
		values := make([]uint64, len(fields)-1)
		for index, field := range fields[1:] {
			values[index], _ = strconv.ParseUint(field, 10, 64)
		}
		user, nice, system, idle, iowait := values[0], values[1], values[2], values[3], values[4]
		result := cpuTimes{idle: idle + iowait, user: user + nice, system: system, iowait: iowait}
		end := 8
		if len(values) < end {
			end = len(values)
		}
		for _, value := range values[:end] {
			result.total += value
		}
		return result, nil
	}
	return cpuTimes{}, fmt.Errorf("cpu statistics not found")
}

type memoryInfo struct {
	total    uint64
	used     uint64
	swapUsed uint64
}

func readMemory() (memoryInfo, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return memoryInfo{}, err
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		values[key] = value * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	used := uint64(0)
	if total > available {
		used = total - available
	}
	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]
	swapUsed := uint64(0)
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}
	if total == 0 {
		return memoryInfo{}, fmt.Errorf("memory information not found")
	}
	return memoryInfo{total: total, used: used, swapUsed: swapUsed}, nil
}

type diskInfo struct {
	total uint64
	used  uint64
}

func readDisk(path string) (diskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskInfo{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := uint64(0)
	if total > available {
		used = total - available
	}
	return diskInfo{total: total, used: used}, nil
}

func readNetwork() (netCounters, []domain.NetworkInterface, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return netCounters{}, nil, err
	}
	defer file.Close()
	byName := map[string]domain.NetworkInterface{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" || name == "" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		byName[name] = domain.NetworkInterface{Name: name, RxBytes: rx, TxBytes: tx}
	}
	interfaces, _ := net.Interfaces()
	var counters netCounters
	result := make([]domain.NetworkInterface, 0, len(byName))
	for _, item := range interfaces {
		if item.Flags&net.FlagLoopback != 0 {
			continue
		}
		countersItem, ok := byName[item.Name]
		if !ok {
			continue
		}
		counters.rxBytes += countersItem.RxBytes
		counters.txBytes += countersItem.TxBytes
		addrs, _ := item.Addrs()
		addresses := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addresses = append(addresses, addr.String())
		}
		countersItem.Addresses = addresses
		countersItem.MAC = item.HardwareAddr.String()
		result = append(result, countersItem)
	}
	return counters, result, nil
}

func readLoad() (float64, float64, float64) {
	body, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(body))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15
}

func readUptime() int64 {
	body, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	return int64(value)
}

func countProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := strconv.Atoi(entry.Name()); err == nil {
				count++
			}
		}
	}
	return count
}

func countTCP() int {
	count := 0
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		if len(lines) > 1 {
			count += len(lines) - 1
		}
	}
	return count
}

func readOSName() string {
	body, err := os.ReadFile("/etc/os-release")
	if err != nil {
		body, err = os.ReadFile("/usr/lib/os-release")
	}
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

func readKernel() string {
	body, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func readCPUModel() string {
	body, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Model") {
			_, value, ok := strings.Cut(line, ":")
			if ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func localAddress() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", time.Second)
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			return addr.IP.String()
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, item := range interfaces {
		if item.Flags&net.FlagLoopback != 0 || item.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := item.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return ""
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round(float64(used)/float64(total)*100, 2)
}

func round(value float64, digits int) float64 {
	factor := 1.0
	for i := 0; i < digits; i++ {
		factor *= 10
	}
	return float64(int64(value*factor+0.5)) / factor
}
