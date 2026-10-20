package model

import "testing"

// linkDefinition 把默认看板的第一个面板换成带指定面板链接的时序面板，便于逐项验证链接校验。
func linkDefinition(panel DashboardPanelLink) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Links = []DashboardPanelLink{panel}
	return definition
}

func TestDashboardPanelLinkIconsAcceptDeclaredValues(t *testing.T) {
	for _, icon := range append([]string{""}, DashboardLinkIcons...) {
		name := icon
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			definition := linkDefinition(DashboardPanelLink{Title: "Wiki", URL: "https://example.test/runbook", Icon: icon})
			if err := ValidateDashboard(definition); err != nil {
				t.Fatalf("declared icon %q rejected: %v", icon, err)
			}
		})
	}
}

func TestDashboardPanelLinkRejectsUnknownIcon(t *testing.T) {
	definition := linkDefinition(DashboardPanelLink{Title: "Wiki", URL: "https://example.test/runbook", Icon: "rocket"})
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected an unknown panel link icon to be rejected")
	}
}

func TestDashboardLinksValidateIcons(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Links = []DashboardPanelLink{{Title: "Wiki", URL: "/admin/metrics", Icon: "docs"}}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("declared dashboard link icon rejected: %v", err)
	}
	definition.Links[0].Icon = "rocket"
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected an unknown dashboard link icon to be rejected")
	}
}

func TestDashboardLinksEnforceCountLimits(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	for range MaxDashboardLinks {
		definition.Links = append(definition.Links, DashboardPanelLink{Title: "Wiki", URL: "/admin/metrics"})
	}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("dashboard links at the limit rejected: %v", err)
	}
	definition.Links = append(definition.Links, DashboardPanelLink{Title: "Wiki", URL: "/admin/metrics"})
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected too many dashboard links to be rejected")
	}
	panel := linkDefinition(DashboardPanelLink{Title: "Wiki", URL: "/admin/metrics"})
	for range MaxDashboardPanelLinks - 1 {
		panel.Panels[0].Links = append(panel.Panels[0].Links, DashboardPanelLink{Title: "Wiki", URL: "/admin/metrics"})
	}
	if err := ValidateDashboard(panel); err != nil {
		t.Fatalf("panel links at the limit rejected: %v", err)
	}
	panel.Panels[0].Links = append(panel.Panels[0].Links, DashboardPanelLink{Title: "Wiki", URL: "/admin/metrics"})
	if err := ValidateDashboard(panel); err == nil {
		t.Fatal("expected too many panel links to be rejected")
	}
}
