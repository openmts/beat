package metriccatalog

import (
	"github.com/beat/backend/internal/model"
)

func Accelerators() []model.MetricDefinition {
	seeds := []metricSeed{{"gpu_utilization_percent", "GPU utilization", "percent"}, {"gpu_memory_used_bytes", "GPU memory used", "bytes"}, {"gpu_memory_total_bytes", "GPU memory capacity", "bytes"}, {"gpu_temperature_celsius", "GPU temperature", "celsius"}, {"gpu_power_watts", "GPU power", "watts"}, {"gpu_power_limit_watts", "GPU power limit", "watts"}}
	result := make([]model.MetricDefinition, 0, len(seeds))
	for _, seed := range seeds {
		value := builtinDefinition(seed)
		value.Tags = []string{"node", "device", "vendor"}
		switch seed.unit {
		case "bytes":
			value.Maximum = numberBound(1 << 50)
		case "celsius":
			value.Minimum = numberBound(-100)
			value.Maximum = numberBound(250)
		case "watts":
			value.Maximum = numberBound(1000000)
		}
		result = append(result, value)
	}
	return result
}
