package acceleratorcollect

import (
	"slices"

	"github.com/beat/backend/internal/model"
)

func collectionState(capabilities []model.AcceleratorCapability) (string, string) {
	success, partial, unavailable := false, false, false
	for _, capability := range capabilities {
		if capability.State == "ok" || capability.State == "partial" {
			success = true
		}
		if capability.State != "ok" {
			partial = true
		}
		if capability.State == "unavailable" {
			unavailable = true
		}
	}
	if success {
		if partial {
			return "partial", "partial_metrics"
		}
		return "ok", ""
	}
	if unavailable {
		return "unavailable", "collector_failed"
	}
	return "unsupported", "collector_missing"
}

func filterMetrics(report *model.AcceleratorReport, allowed []string) {
	for index := range report.Devices {
		device := &report.Devices[index]
		for key := range device.Metrics {
			if !slices.Contains(allowed, key) {
				delete(device.Metrics, key)
			}
		}
		device.Errors = slices.DeleteFunc(device.Errors, func(issue model.AcceleratorMetricError) bool { return !slices.Contains(allowed, issue.Metric) })
	}
}
