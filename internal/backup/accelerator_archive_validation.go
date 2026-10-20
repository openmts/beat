package backup

import (
	"math"
	"slices"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func validAcceleratorArchiveRecord(row metricRecord, definitions map[string]model.MetricDefinition) bool {
	if definitions == nil || row.TimestampNS == math.MaxInt64 {
		return false
	}
	if row.Measurement == model.AcceleratorSampleMeasurement {
		if !exactKeys(row.Tags, "node") {
			return false
		}
		if _, ok := definitions[model.AcceleratorMetricKeys[0]]; !ok {
			return false
		}
		_, err := store.DecodeAcceleratorSample(mts.Row{Tags: row.Tags, Fields: row.Fields, Timestamp: row.TimestampNS})
		return err == nil
	}
	if !exactKeys(row.Tags, "node", "device", "vendor") || !model.ValidAcceleratorDeviceID(row.Tags["device"]) || !model.ValidAcceleratorVendor(row.Tags["vendor"]) {
		return false
	}
	definition, exists := definitions[row.Measurement]
	if !exists || !validAcceleratorArchiveDefinition(definition) {
		return false
	}
	if !exactFields(row.Fields, map[string]mts.FieldType{"value": mts.FieldFloat64}) {
		return false
	}
	values := map[string]float64{row.Measurement: row.Fields["value"].Float64}
	return model.ValidateAcceleratorValues(values, model.AcceleratorMetricKeys) == nil && validScalarMetricRecord(row, definitions)
}

func validAcceleratorArchiveDefinition(definition model.MetricDefinition) bool {
	if definition.Source != "builtin" || definition.Kind != "gauge" || len(definition.Tags) != 3 {
		return false
	}
	for _, tag := range []string{"node", "device", "vendor"} {
		if !slices.Contains(definition.Tags, tag) {
			return false
		}
	}
	units := map[string]string{"gpu_utilization_percent": "percent", "gpu_memory_used_bytes": "bytes", "gpu_memory_total_bytes": "bytes", "gpu_temperature_celsius": "celsius", "gpu_power_watts": "watts", "gpu_power_limit_watts": "watts"}
	unit, exists := units[definition.Key]
	return exists && definition.Unit == unit
}
