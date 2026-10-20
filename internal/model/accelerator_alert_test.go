package model

import "testing"

func TestAcceleratorAlertDerivedDefinitions(t *testing.T) {
	for _, key := range AcceleratorMetricKeys {
		if !IsAcceleratorAlertMetric(key) || len(AcceleratorAlertInputs(key)) != 1 {
			t.Fatal(key)
		}
	}
	if IsAcceleratorAlertMetric("cpu") || AcceleratorAlertDefinition("cpu") != nil {
		t.Fatal("scalar was classified as GPU")
	}
	for _, key := range []string{MetricGPUMemoryPercent, MetricGPUPowerLimitPercent} {
		definition := AcceleratorAlertDefinition(key)
		if !IsAcceleratorAlertMetric(key) || len(AcceleratorAlertInputs(key)) != 2 || definition == nil || definition.Unit != "percent" || definition.Minimum == nil || *definition.Minimum != 0 {
			t.Fatal(key, definition)
		}
		if key == MetricGPUMemoryPercent && (definition.Maximum == nil || *definition.Maximum != 100) {
			t.Fatal("memory ratio lacks bound")
		}
		if key == MetricGPUPowerLimitPercent && definition.Maximum != nil {
			t.Fatal("power above a configured limit must remain representable")
		}
	}
}
