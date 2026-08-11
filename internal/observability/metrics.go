package observability

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type requestKey struct {
	method string
	route  string
}

type statusKey struct {
	requestKey
	status int
}

type requestStats struct {
	count   uint64
	sum     float64
	buckets []uint64
}

type Registry struct {
	startedAt time.Time
	mu        sync.RWMutex
	requests  map[requestKey]*requestStats
	statuses  map[statusKey]uint64
}

func NewRegistry() *Registry {
	return &Registry{
		startedAt: time.Now(), requests: make(map[requestKey]*requestStats),
		statuses: make(map[statusKey]uint64),
	}
}

func (registry *Registry) ObserveHTTP(method, route string, status int, duration time.Duration) {
	key := requestKey{method: normalizedMethod(method), route: normalizedRoute(route)}
	seconds := duration.Seconds()
	registry.mu.Lock()
	stats := registry.requests[key]
	if stats == nil {
		stats = &requestStats{buckets: make([]uint64, len(latencyBuckets))}
		registry.requests[key] = stats
	}
	stats.count++
	stats.sum += seconds
	for index, boundary := range latencyBuckets {
		if seconds <= boundary {
			stats.buckets[index]++
		}
	}
	registry.statuses[statusKey{requestKey: key, status: status}]++
	registry.mu.Unlock()
}

func (registry *Registry) Prometheus() string {
	registry.mu.RLock()
	requests := cloneRequests(registry.requests)
	statuses := cloneStatuses(registry.statuses)
	registry.mu.RUnlock()
	var output strings.Builder
	writeHTTPMetrics(&output, requests, statuses)
	writeProcessMetrics(&output, registry.startedAt)
	return output.String()
}

func cloneRequests(source map[requestKey]*requestStats) map[requestKey]requestStats {
	result := make(map[requestKey]requestStats, len(source))
	for key, stats := range source {
		copyStats := *stats
		copyStats.buckets = append([]uint64(nil), stats.buckets...)
		result[key] = copyStats
	}
	return result
}

func cloneStatuses(source map[statusKey]uint64) map[statusKey]uint64 {
	result := make(map[statusKey]uint64, len(source))
	for key, count := range source {
		result[key] = count
	}
	return result
}

func writeHTTPMetrics(
	output *strings.Builder,
	requests map[requestKey]requestStats,
	statuses map[statusKey]uint64,
) {
	output.WriteString("# TYPE beat_http_requests_total counter\n")
	statusKeys := sortedStatusKeys(statuses)
	for _, key := range statusKeys {
		fmt.Fprintf(output, "beat_http_requests_total{method=%s,route=%s,status=%q} %d\n",
			quoteLabel(key.method), quoteLabel(key.route), strconv.Itoa(key.status), statuses[key])
	}
	output.WriteString("# TYPE beat_http_request_duration_seconds histogram\n")
	requestKeys := sortedRequestKeys(requests)
	for _, key := range requestKeys {
		writeRequestHistogram(output, key, requests[key])
	}
}

func writeRequestHistogram(output *strings.Builder, key requestKey, stats requestStats) {
	labels := "method=" + quoteLabel(key.method) + ",route=" + quoteLabel(key.route)
	for index, boundary := range latencyBuckets {
		fmt.Fprintf(output, "beat_http_request_duration_seconds_bucket{%s,le=%q} %d\n",
			labels, strconv.FormatFloat(boundary, 'g', -1, 64), stats.buckets[index])
	}
	fmt.Fprintf(output, "beat_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, stats.count)
	fmt.Fprintf(output, "beat_http_request_duration_seconds_sum{%s} %g\n", labels, stats.sum)
	fmt.Fprintf(output, "beat_http_request_duration_seconds_count{%s} %d\n", labels, stats.count)
}

func writeProcessMetrics(output *strings.Builder, startedAt time.Time) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	output.WriteString("# TYPE beat_process_uptime_seconds gauge\n")
	fmt.Fprintf(output, "beat_process_uptime_seconds %g\n", time.Since(startedAt).Seconds())
	output.WriteString("# TYPE beat_process_goroutines gauge\n")
	fmt.Fprintf(output, "beat_process_goroutines %d\n", runtime.NumGoroutine())
	output.WriteString("# TYPE beat_process_memory_bytes gauge\n")
	fmt.Fprintf(output, "beat_process_memory_bytes %d\n", memory.Alloc)
}

func sortedRequestKeys(values map[requestKey]requestStats) []requestKey {
	keys := make([]requestKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].method < keys[j].method ||
			(keys[i].method == keys[j].method && keys[i].route < keys[j].route)
	})
	return keys
}

func sortedStatusKeys(values map[statusKey]uint64) []statusKey {
	keys := make([]statusKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})
	return keys
}

func normalizedMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}

func normalizedRoute(route string) string {
	if route == "" {
		return "unmatched"
	}
	if len(route) > 160 {
		return "oversized"
	}
	return route
}

func quoteLabel(value string) string {
	return strconv.Quote(value)
}
