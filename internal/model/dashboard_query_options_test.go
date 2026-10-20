package model

import (
	"errors"
	"testing"
)

func TestDashboardQueryBudgetAcceptsBounds(t *testing.T) {
	for _, value := range []*int{nil, dashboardInteger(MinDashboardQueryPoints), dashboardInteger(MaxDashboardQueryPoints)} {
		definition := DefaultMetricDashboard("admin").Definition
		definition.MaxPoints = value
		definition.Panels[0].MaxPoints = value
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("valid query budget rejected: %v", err)
		}
	}
	for _, value := range []*int{dashboardInteger(0), dashboardInteger(MaxDashboardQueryPoints + 1)} {
		definition := DefaultMetricDashboard("admin").Definition
		definition.Panels[0].MaxPoints = value
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("panel budget %d accepted: %v", *value, err)
		}
		definition = DefaultMetricDashboard("admin").Definition
		definition.MaxPoints = value
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("dashboard budget %d accepted: %v", *value, err)
		}
	}
}

func TestDashboardNowDelayBounds(t *testing.T) {
	for _, value := range []*int{nil, dashboardInteger(0), dashboardInteger(MaxDashboardNowDelaySeconds)} {
		definition := DefaultMetricDashboard("admin").Definition
		definition.NowDelaySeconds = value
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("valid now delay rejected: %v", err)
		}
	}
	for _, value := range []*int{dashboardInteger(-1), dashboardInteger(MaxDashboardNowDelaySeconds + 1)} {
		definition := DefaultMetricDashboard("admin").Definition
		definition.NowDelaySeconds = value
		if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("now delay %d accepted: %v", *value, err)
		}
	}
}
