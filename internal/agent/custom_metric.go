package agent

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

// registeredMetric is a custom metric the agent is responsible for collecting.
type registeredMetric struct {
	Name            string
	ShellCommand    string
	IntervalSeconds int
}

// CustomMetricRegistry holds the custom metrics registered on this agent. The
// registry is replaced in full whenever the server sends a register task.
type CustomMetricRegistry struct {
	mu      sync.RWMutex
	metrics []registeredMetric
}

func NewCustomMetricRegistry() *CustomMetricRegistry {
	return &CustomMetricRegistry{}
}

// Replace sets the full set of registered metrics.
func (r *CustomMetricRegistry) Replace(metrics []model.CustomMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = make([]registeredMetric, 0, len(metrics))
	for _, metric := range metrics {
		r.metrics = append(r.metrics, registeredMetric{
			Name:            metric.Name,
			ShellCommand:    metric.ShellCommand,
			IntervalSeconds: metric.IntervalSeconds,
		})
	}
}

// All returns a snapshot of the registered metrics.
func (r *CustomMetricRegistry) All() []registeredMetric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]registeredMetric, len(r.metrics))
	copy(out, r.metrics)
	return out
}

// Collect runs the shell command for a registered metric and parses a single
// numeric value from its stdout.
func CollectMetric(ctx context.Context, metric registeredMetric) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "-c", metric.ShellCommand)
	output, err := command.Output()
	if err != nil {
		return 0, false
	}
	value, err := parseMetricValue(string(output))
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseMetricValue(output string) (float64, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return 0, errMetricValue
	}
	// Take the first whitespace-separated token.
	fields := strings.Fields(trimmed)
	return strconv.ParseFloat(fields[0], 64)
}

var errMetricValue = errors.New("metric value is not a number")

// CustomMetricCollector periodically samples all registered metrics.
type CustomMetricCollector struct {
	registry   *CustomMetricRegistry
	now        func() time.Time
	lastSample map[string]int64
}

func NewCustomMetricCollector(registry *CustomMetricRegistry) *CustomMetricCollector {
	return &CustomMetricCollector{
		registry:   registry,
		now:        func() time.Time { return time.Now() },
		lastSample: map[string]int64{},
	}
}

// Sample executes every registered metric whose interval has elapsed since the
// previous sample, returning name/value pairs for successful collections.
func (c *CustomMetricCollector) Sample(ctx context.Context) map[string]float64 {
	now := c.now().Unix()
	results := map[string]float64{}
	for _, metric := range c.registry.All() {
		if metric.IntervalSeconds <= 0 {
			continue
		}
		interval := int64(metric.IntervalSeconds)
		if last, ok := c.lastSample[metric.Name]; ok && now-last < interval {
			continue
		}
		if value, ok := CollectMetric(ctx, metric); ok {
			results[metric.Name] = value
			c.lastSample[metric.Name] = now
		}
	}
	return results
}
