package model

import "testing"

// tableDefinition 把默认看板的第一个面板换成表格面板，便于验证表格专属选项。
func tableDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type, panel.Legend = "table", false
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardCellDisplayModeAcceptsDeclaredValues(t *testing.T) {
	modes := append([]string{""}, DashboardCellDisplayModes...)
	for _, mode := range modes {
		name := mode
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			definition := tableDefinition(func(panel *DashboardPanel) {
				if mode != "" {
					panel.CellDisplayMode = &mode
				}
			})
			if err := ValidateDashboard(definition); err != nil {
				t.Fatalf("declared cell display mode %q rejected: %v", mode, err)
			}
		})
	}
}

func TestDashboardCellDisplayModeRejectsUnknownValue(t *testing.T) {
	mode := "sparkle"
	definition := tableDefinition(func(panel *DashboardPanel) { panel.CellDisplayMode = &mode })
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected an unknown cell display mode to be rejected")
	}
}

func TestDashboardCellDisplayModeIsRejectedOffTablePanels(t *testing.T) {
	mode := "color_background"
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].CellDisplayMode = &mode
	if definition.Panels[0].Type == "table" {
		t.Fatalf("default panel type %q cannot exercise this rule", definition.Panels[0].Type)
	}
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected a cell display mode on a non-table panel to be rejected")
	}
}

func TestDashboardTableOrientationAcceptsDeclaredValues(t *testing.T) {
	orientations := append([]string{""}, DashboardTableOrientations...)
	for _, orientation := range orientations {
		name := orientation
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			definition := tableDefinition(func(panel *DashboardPanel) {
				if orientation != "" {
					panel.TableOrientation = &orientation
				}
			})
			if err := ValidateDashboard(definition); err != nil {
				t.Fatalf("declared table orientation %q rejected: %v", orientation, err)
			}
		})
	}
}

func TestDashboardTableOrientationRejectsUnknownValue(t *testing.T) {
	orientation := "diagonal"
	definition := tableDefinition(func(panel *DashboardPanel) { panel.TableOrientation = &orientation })
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected an unknown table orientation to be rejected")
	}
}

func TestDashboardTableOrientationIsRejectedOffTablePanels(t *testing.T) {
	orientation := "wide"
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].TableOrientation = &orientation
	if definition.Panels[0].Type == "table" {
		t.Fatalf("default panel type %q cannot exercise this rule", definition.Panels[0].Type)
	}
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected a table orientation on a non-table panel to be rejected")
	}
}

func TestDashboardTableOrientationCoexistsWithCellDisplayMode(t *testing.T) {
	orientation, mode := "wide", "gradient"
	definition := tableDefinition(func(panel *DashboardPanel) {
		panel.TableOrientation = &orientation
		panel.CellDisplayMode = &mode
	})
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("wide table with a cell display mode rejected: %v", err)
	}
}
