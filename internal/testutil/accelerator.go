package testutil

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// AcceleratorReport 是合成GPUfixture，不读取硬件或vendor工具。
func AcceleratorReport(at time.Time) model.AcceleratorReport {
	return model.AcceleratorReport{Version: 1, SampleID: uuid.NewString(), CapturedAt: at.UTC(), CadenceMS: 5000, CollectorVersion: "1", InstallationEnabled: true, State: "ok",
		Capabilities: []model.AcceleratorCapability{{Vendor: "nvidia", State: "ok", Metrics: slices.Clone(model.AcceleratorMetricKeys)}},
		Devices:      []model.AcceleratorDeviceReport{{Fingerprint: strings.Repeat("a", 64), Vendor: "nvidia", Model: "Synthetic GPU", DriverVersion: "fixture", Capabilities: slices.Clone(model.AcceleratorMetricKeys), Metrics: map[string]float64{"gpu_utilization_percent": 73.125, "gpu_memory_used_bytes": 1024, "gpu_memory_total_bytes": 4096, "gpu_temperature_celsius": 45, "gpu_power_watts": 123, "gpu_power_limit_watts": 250}}}}
}
