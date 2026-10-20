package agent

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	maxCustomMetricDefinitions     = 64
	maxCustomMetricNameBytes       = 100
	maxCustomMetricCommandBytes    = 2048
	minCustomMetricInterval        = 5
	maxCustomMetricInterval        = 86400
	maxCustomMetricRegisterPayload = 256 << 10
)

type registeredMetric struct {
	ID                string
	Name              string
	ShellCommand      string
	IntervalSeconds   int
	DefinitionVersion string
	version           uint64
}

type CustomMetricRegistry struct {
	mu             sync.RWMutex
	metrics        []registeredMetric
	version        uint64
	sequence       uint64
	metricVersions map[string]uint64
	revision       time.Time
	changed        chan struct{}
}

func NewCustomMetricRegistry() *CustomMetricRegistry {
	return &CustomMetricRegistry{changed: make(chan struct{}), metricVersions: make(map[string]uint64)}
}

func (registry *CustomMetricRegistry) Replace(metrics []model.CustomMetric) error {
	return registry.replace(metrics, time.Time{}, false)
}

func (registry *CustomMetricRegistry) replace(metrics []model.CustomMetric, revision time.Time, ordered bool) error {
	values, err := validateRegisteredMetrics(metrics)
	if err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if ordered && !registry.revision.IsZero() && !revision.After(registry.revision) {
		return nil
	}
	if ordered {
		registry.revision = revision
	}
	if !registry.updateMetricsLocked(values) {
		return nil
	}
	registry.version++
	if registry.changed != nil {
		close(registry.changed)
	}
	registry.changed = make(chan struct{})
	return nil
}

func (registry *CustomMetricRegistry) updateMetricsLocked(values []registeredMetric) bool {
	previous := make(map[string]registeredMetric, len(registry.metrics))
	for _, metric := range registry.metrics {
		previous[metric.Name] = metric
	}
	changed := len(values) != len(registry.metrics)
	versions := make(map[string]uint64, len(values))
	for index, metric := range values {
		if old, ok := previous[metric.Name]; ok && sameCustomMetric(old, metric) {
			metric.version = old.version
		} else {
			registry.sequence++
			metric.version = registry.sequence
			changed = true
		}
		values[index] = metric
		versions[metric.Name] = metric.version
	}
	registry.metrics, registry.metricVersions = values, versions
	return changed
}

func (registry *CustomMetricRegistry) All() []registeredMetric {
	metrics, _, _ := registry.snapshot()
	return metrics
}

func (registry *CustomMetricRegistry) snapshot() ([]registeredMetric, uint64, <-chan struct{}) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	metrics := append([]registeredMetric{}, registry.metrics...)
	return metrics, registry.version, registry.changed
}

func validateRegisteredMetrics(metrics []model.CustomMetric) ([]registeredMetric, error) {
	if len(metrics) > maxCustomMetricDefinitions {
		return nil, errors.New("a node supports at most 64 custom metrics")
	}
	values := make([]registeredMetric, 0, len(metrics))
	seen := make(map[string]bool, len(metrics))
	for _, metric := range metrics {
		if !metric.Enabled {
			continue
		}
		if !validCustomMetricName(metric.Name) || seen[metric.Name] {
			return nil, errors.New("invalid or duplicate custom metric name")
		}
		if !validCustomMetricCommand(metric.ShellCommand) {
			return nil, errors.New("custom metric command must contain 1 to 2048 bytes without NUL")
		}
		if metric.IntervalSeconds < minCustomMetricInterval || metric.IntervalSeconds > maxCustomMetricInterval {
			return nil, errors.New("custom metric interval must be between 5 and 86400 seconds")
		}
		seen[metric.Name] = true
		values = append(values, registeredMetric{ID: metric.ID, Name: metric.Name, ShellCommand: metric.ShellCommand, IntervalSeconds: metric.IntervalSeconds, DefinitionVersion: model.CustomMetricDefinitionVersion(metric)})
	}
	return values, nil
}

func validCustomMetricCommand(command string) bool {
	return strings.TrimSpace(command) != "" && len(command) <= maxCustomMetricCommandBytes && !strings.ContainsRune(command, 0)
}

func sameCustomMetric(first, second registeredMetric) bool {
	return first.ID == second.ID && first.Name == second.Name && first.ShellCommand == second.ShellCommand && first.IntervalSeconds == second.IntervalSeconds && first.DefinitionVersion == second.DefinitionVersion
}

func validCustomMetricName(name string) bool {
	if name == "" || len(name) > maxCustomMetricNameBytes {
		return false
	}
	for _, character := range name {
		if !validCustomMetricCharacter(character) {
			return false
		}
	}
	return true
}

func validCustomMetricCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-'
}
