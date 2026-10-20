package runtimediag

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maximumStatusBytes = 64 << 10

// CaptureState 描述捕获子系统的可用性与并发状态。
type CaptureState struct {
	Active   int  `json:"active"`
	Busy     bool `json:"busy"`
	Degraded bool `json:"degraded"`
}

// MemorySummary 只包含数值内存分类，不含路径、命令或环境信息。
type MemorySummary struct {
	HeapAllocBytes  uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	SystemBytes     uint64 `json:"system_bytes"`
}

// SchedulerSummary 描述受控的调度与线程计数。
type SchedulerSummary struct {
	GOMAXPROCS int    `json:"gomaxprocs"`
	Goroutines int    `json:"goroutines"`
	OSThreads  uint64 `json:"os_threads"`
	CgoCalls   int64  `json:"cgo_calls"`
}

// GCSummary 汇总 GC 次数与暂停时间。
type GCSummary struct {
	Cycles           uint32  `json:"cycles"`
	PauseTotalSecond float64 `json:"pause_total_seconds"`
	LastPauseSecond  float64 `json:"last_pause_seconds"`
}

// Summary 是所有者可见的运行时摘要；它不含环境变量、命令行、路径或账号数据。
type Summary struct {
	Version       string           `json:"version"`
	Commit        string           `json:"commit"`
	GoVersion     string           `json:"go_version"`
	StartedAt     time.Time        `json:"started_at"`
	UptimeSeconds float64          `json:"uptime_seconds"`
	Readiness     string           `json:"readiness"`
	Capture       CaptureState     `json:"capture"`
	Memory        MemorySummary    `json:"memory"`
	Scheduler     SchedulerSummary `json:"scheduler"`
	GC            GCSummary        `json:"gc"`
}

// Summary 生成当前运行时摘要，并顺带清理过期产物。
func (service *Service) Summary() Summary {
	service.mu.Lock()
	service.sweepLocked(service.now())
	state := CaptureState{Active: service.active, Busy: service.active > 0, Degraded: service.cleanupFailures > 0}
	service.mu.Unlock()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	now := service.now()
	return Summary{Version: service.config.Version, Commit: service.config.Commit, GoVersion: runtime.Version(),
		StartedAt: service.config.StartedAt, UptimeSeconds: now.Sub(service.config.StartedAt).Seconds(),
		Readiness: service.readiness(), Capture: state, Memory: memorySummary(stats),
		Scheduler: SchedulerSummary{GOMAXPROCS: runtime.GOMAXPROCS(0), Goroutines: runtime.NumGoroutine(),
			OSThreads: osThreads(), CgoCalls: runtime.NumCgoCall()},
		GC: GCSummary{Cycles: stats.NumGC, PauseTotalSecond: float64(stats.PauseTotalNs) / float64(time.Second),
			LastPauseSecond: lastPauseSeconds(stats)}}
}

func (service *Service) readiness() string {
	if service.config.Readiness == nil {
		return "ok"
	}
	if err := service.config.Readiness(); err != nil {
		return "degraded"
	}
	return "ok"
}

func memorySummary(stats runtime.MemStats) MemorySummary {
	return MemorySummary{HeapAllocBytes: stats.HeapAlloc, HeapInuseBytes: stats.HeapInuse,
		StackInuseBytes: stats.StackInuse, SystemBytes: stats.Sys}
}

func lastPauseSeconds(stats runtime.MemStats) float64 {
	if stats.NumGC == 0 {
		return 0
	}
	return float64(stats.PauseNs[(stats.NumGC-1)%uint32(len(stats.PauseNs))]) / float64(time.Second)
}

// osThreads 读取 Linux 线程计数；其他平台或读取失败时返回 0，摘要仍然可用。
func osThreads() uint64 {
	content, err := os.ReadFile("/proc/self/status")
	if err != nil || len(content) > maximumStatusBytes {
		return 0
	}
	for _, line := range strings.Split(string(content), "\n") {
		value, found := strings.CutPrefix(line, "Threads:")
		if !found {
			continue
		}
		threads, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return 0
		}
		return threads
	}
	return 0
}
