package model

import (
	"fmt"
	"slices"
	"strings"
)

func ValidateDashboardPolicy(definition DashboardDefinition, policy DashboardPolicy) error {
	if err := ValidateDashboard(definition); err != nil {
		return err
	}
	for _, panel := range definition.Panels {
		if err := validatePanelPolicy(panel, policy); err != nil {
			return err
		}
	}
	return nil
}

func validatePanelPolicy(panel DashboardPanel, policy DashboardPolicy) error {
	unit := ""
	for _, series := range panel.Series {
		metric, exists := policy.Metrics[series.Metric]
		if !dashboardMetricAvailable(metric, exists, policy.Public) {
			return fmt.Errorf("%w: selected metric is unavailable", ErrInvalidDashboard)
		}
		if err := validateSeriesPolicy(series, metric, policy.Nodes); err != nil {
			return err
		}
		effectiveUnit := DashboardSeriesUnit(metric.Unit, series.Aggregation)
		if unit != "" && unit != effectiveUnit {
			return fmt.Errorf("%w: panel series must use compatible units", ErrInvalidDashboard)
		}
		unit = effectiveUnit
	}
	if panel.Unit != "" && panel.Unit != "auto" && panel.Unit != unit {
		return fmt.Errorf("%w: panel unit does not match its series", ErrInvalidDashboard)
	}
	return nil
}

func dashboardMetricAvailable(metric DashboardMetricPolicy, exists, public bool) bool {
	return exists && metric.Enabled && (!public || metric.Public)
}

func validateSeriesPolicy(series DashboardSeries, metric DashboardMetricPolicy, nodes map[string]bool) error {
	if !slices.Contains(metric.Aggregations, series.Aggregation) {
		return fmt.Errorf("%w: aggregation is not supported by this metric", ErrInvalidDashboard)
	}
	if !strings.HasPrefix(series.NodeID, "$") && !nodes[series.NodeID] {
		return fmt.Errorf("%w: selected node is unavailable", ErrInvalidDashboard)
	}
	return nil
}

func DashboardSeriesUnit(unit, aggregation string) string {
	if aggregation == "rate" {
		return unit + "_per_second"
	}
	return unit
}
