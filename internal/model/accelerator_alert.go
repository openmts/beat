package model

import "slices"

const (
	MetricGPUMemoryPercent       = "gpu_memory_percent"
	MetricGPUPowerLimitPercent   = "gpu_power_limit_percent"
	AcceleratorAnyDevice         = "*"
	AcceleratorDeviceUnavailable = "device_unavailable"
)

func IsAcceleratorAlertMetric(key string) bool {
	return slices.Contains(AcceleratorMetricKeys, key) || key == MetricGPUMemoryPercent || key == MetricGPUPowerLimitPercent
}

func AcceleratorAlertInputs(key string) []string {
	switch key {
	case MetricGPUMemoryPercent:
		return []string{"gpu_memory_used_bytes", "gpu_memory_total_bytes"}
	case MetricGPUPowerLimitPercent:
		return []string{"gpu_power_watts", "gpu_power_limit_watts"}
	default:
		return []string{key}
	}
}

// 派生比例仅供告警评估，不创建新的MTS测量或SQLite数值。
func AcceleratorAlertDefinition(key string) *MetricDefinition {
	if key != MetricGPUMemoryPercent && key != MetricGPUPowerLimitPercent {
		return nil
	}
	minimum, maximum := 0.0, 100.0
	definition := &MetricDefinition{Key: key, Label: "GPU memory usage", Source: "derived", Kind: "gauge", Unit: "percent", Enabled: true, Aggregations: []string{"last", "avg", "min", "max"}, DefaultAggregation: "last", Minimum: &minimum}
	if key == MetricGPUMemoryPercent {
		definition.Maximum = &maximum
	} else {
		definition.Label = "GPU power limit usage"
	}
	return definition
}
