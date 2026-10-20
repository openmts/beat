package observability

import (
	"fmt"
	"slices"
	"strings"
)

// 运行时诊断指标只接受固定枚举标签；未知取值直接丢弃，避免无界序列。
var (
	runtimeCaptureKinds   = []string{"cpu", "trace", "heap", "allocs", "goroutine", "mutex", "block", "threadcreate"}
	runtimeCaptureResults = []string{"started", "completed", "failed", "cancelled", "expired", "downloaded", "rejected"}
	runtimeCaptureReasons = []string{"conflict", "rate_limited", "invalid_kind", "invalid_duration", "capacity", "size_limit", "writer_failed", "not_found"}
)

type runtimeEventKey struct{ kind, result string }

// RuntimeDiagnosticsStatistics 是抓取时读取的占用快照。
type RuntimeDiagnosticsStatistics struct {
	Active          int
	Bytes           int64
	CleanupFailures uint64
}

type runtimeDiagnosticsSnapshot struct {
	events     map[runtimeEventKey]uint64
	rejections map[string]uint64
	stats      RuntimeDiagnosticsStatistics
}

// ObserveRuntimeCaptureEvent 记录一次捕获生命周期事件；未知 kind/result 被忽略。
func (registry *Registry) ObserveRuntimeCaptureEvent(kind, result string) {
	if !slices.Contains(runtimeCaptureKinds, kind) || !slices.Contains(runtimeCaptureResults, result) {
		return
	}
	registry.mu.Lock()
	registry.runtimeEvents[runtimeEventKey{kind: kind, result: result}]++
	registry.mu.Unlock()
}

// ObserveRuntimeCaptureRejection 记录被拒绝的捕获请求原因；未知原因被忽略。
func (registry *Registry) ObserveRuntimeCaptureRejection(reason string) {
	if !slices.Contains(runtimeCaptureReasons, reason) {
		return
	}
	registry.mu.Lock()
	registry.runtimeRejections[reason]++
	registry.mu.Unlock()
}

// SetRuntimeDiagnosticsStatistics 注册占用快照来源；抓取失败时保留上一次成功的快照。
func (registry *Registry) SetRuntimeDiagnosticsStatistics(source func() (RuntimeDiagnosticsStatistics, error)) {
	registry.mu.Lock()
	registry.runtimeSource = source
	registry.mu.Unlock()
}

func (registry *Registry) refreshRuntimeDiagnosticsStatistics() RuntimeDiagnosticsStatistics {
	registry.mu.RLock()
	source := registry.runtimeSource
	registry.mu.RUnlock()
	if source != nil {
		if stats, err := source(); err == nil {
			registry.mu.Lock()
			registry.runtimeStats = stats
			registry.mu.Unlock()
		}
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.runtimeStats
}

func cloneRuntimeEvents(source map[runtimeEventKey]uint64) map[runtimeEventKey]uint64 {
	result := make(map[runtimeEventKey]uint64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func writeRuntimeDiagnosticsMetrics(output *strings.Builder, snapshot runtimeDiagnosticsSnapshot) {
	output.WriteString("# TYPE beat_runtime_capture_events_total counter\n")
	for _, kind := range runtimeCaptureKinds {
		for _, result := range runtimeCaptureResults {
			fmt.Fprintf(output, "beat_runtime_capture_events_total{kind=%s,result=%s} %d\n",
				quoteLabel(kind), quoteLabel(result), snapshot.events[runtimeEventKey{kind: kind, result: result}])
		}
	}
	output.WriteString("# TYPE beat_runtime_capture_rejections_total counter\n")
	for _, reason := range runtimeCaptureReasons {
		fmt.Fprintf(output, "beat_runtime_capture_rejections_total{reason=%s} %d\n",
			quoteLabel(reason), snapshot.rejections[reason])
	}
	output.WriteString("# TYPE beat_runtime_captures_active gauge\n")
	fmt.Fprintf(output, "beat_runtime_captures_active %d\n", snapshot.stats.Active)
	output.WriteString("# TYPE beat_runtime_capture_storage_bytes gauge\n")
	fmt.Fprintf(output, "beat_runtime_capture_storage_bytes %d\n", snapshot.stats.Bytes)
	output.WriteString("# TYPE beat_runtime_capture_cleanup_failures_total counter\n")
	fmt.Fprintf(output, "beat_runtime_capture_cleanup_failures_total %d\n", snapshot.stats.CleanupFailures)
}
