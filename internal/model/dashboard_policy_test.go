package model

import "testing"

func TestDashboardMetricPolicy(t *testing.T) {
	for _, scenario := range []string{"private", "disabled", "missing", "aggregation", "mixed units", "unknown node", "unit override"} {
		t.Run(scenario, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			policy := dashboardTestPolicy()
			metric := policy.Metrics["cpu"]
			switch scenario {
			case "private":
				metric.Public = false
			case "disabled":
				metric.Enabled = false
			case "aggregation":
				metric.Aggregations = []string{"last"}
			case "mixed units":
				definition.Panels[0].Series = append(definition.Panels[0].Series, definition.Panels[3].Series[1])
			case "unknown node":
				definition.Panels[0].Series[0].NodeID = "node-hidden"
			case "unit override":
				definition.Panels[0].Unit = "bytes"
			}
			policy.Metrics["cpu"] = metric
			if scenario == "missing" {
				delete(policy.Metrics, "cpu")
			}
			if err := ValidateDashboardPolicy(definition, policy); err == nil {
				t.Fatal("invalid catalog or node policy accepted")
			}
		})
	}
}

func TestDashboardPrivateNodeAndMetricForAdministrator(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Series[0].NodeID = "node-one"
	definition.Panels[0].Unit = "percent"
	policy := dashboardTestPolicy()
	policy.Public = false
	metric := policy.Metrics["cpu"]
	metric.Public = false
	policy.Metrics["cpu"] = metric
	if err := ValidateDashboardPolicy(definition, policy); err != nil {
		t.Fatal(err)
	}
	if DashboardSeriesUnit("bytes", "rate") != "bytes_per_second" || DashboardSeriesUnit("percent", "avg") != "percent" {
		t.Fatal("counter rate unit is incorrect")
	}
}

func TestDashboardPolicyRejectsInvalidShape(t *testing.T) {
	definition := DefaultMetricDashboard("public").Definition
	definition.Version = 9
	if err := ValidateDashboardPolicy(definition, dashboardTestPolicy()); err == nil {
		t.Fatal("invalid definition bypassed policy validation")
	}
}
