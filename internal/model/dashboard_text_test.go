package model

import (
	"strings"
	"testing"
)

// textDefinition 把默认看板的单面板改造成文本面板，便于逐项验证文本专属校验。
func textDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type, panel.Size, panel.Legend = "text", "large", false
	panel.Unit, panel.Series = "", nil
	panel.Text = "# Runbook\n\n- 检查磁盘\n- 检查网卡"
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardTextAcceptsMarkdownOnlyPayloads(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"markdown": func(panel *DashboardPanel) {},
		"empty":    func(panel *DashboardPanel) { panel.Text = "" },
		"links": func(panel *DashboardPanel) {
			panel.Links = []DashboardPanelLink{{Title: "Wiki", URL: "https://example.test/runbook"}}
		},
		"newlines":  func(panel *DashboardPanel) { panel.Text = "line one\r\nline two\ttabbed" },
		"collapsed": func(panel *DashboardPanel) { panel.Size = "medium" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(textDefinition(change)); err != nil {
				t.Fatalf("valid text panel rejected: %v", err)
			}
		})
	}
}

func TestDashboardTextRejectsQueryOptionsAndBadContent(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"series": func(panel *DashboardPanel) {
			panel.Series = []DashboardSeries{{RefID: "A", NodeID: "$node", Metric: "cpu", Aggregation: "avg", Color: "blue"}}
		},
		"legend":     func(panel *DashboardPanel) { panel.Legend = true },
		"unit":       func(panel *DashboardPanel) { panel.Unit = "percent" },
		"thresholds": func(panel *DashboardPanel) { panel.Thresholds = []DashboardThreshold{{Value: 1, Color: "amber"}} },
		"axes":       func(panel *DashboardPanel) { panel.Min, panel.Max = floatPointer(0), floatPointer(100) },
		"repeat":     func(panel *DashboardPanel) { panel.Repeat = "node" },
		"max points": func(panel *DashboardPanel) { panel.MaxPoints = intPointer(120) },
		"row fields": func(panel *DashboardPanel) { panel.RowCollapsed = boolPointer(true) },
		"transform": func(panel *DashboardPanel) {
			panel.Transformations = []DashboardTransformation{{Kind: "limit", Count: intPointer(2)}}
		},
		"too long":     func(panel *DashboardPanel) { panel.Text = strings.Repeat("a", MaxDashboardTextBytes+1) },
		"control char": func(panel *DashboardPanel) { panel.Text = "bad\u0007content" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(textDefinition(change)); err == nil {
				t.Fatalf("invalid text panel accepted: %s", name)
			}
		})
	}
}

func TestDashboardTextIsRejectedOnQueryPanels(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Text = "note"
	if err := ValidateDashboard(definition); err == nil {
		t.Fatal("expected text content on a timeseries panel to be rejected")
	}
}
