package model

import (
	"errors"
	"strings"
	"testing"
)

func descriptionPanel() DashboardPanel {
	return DashboardPanel{
		ID: "panel-1", Title: "CPU", Type: "stat", Size: "medium",
		Legend: true, Thresholds: []DashboardThreshold{},
		Series: []DashboardSeries{{RefID: "A", NodeID: "node-1", Metric: "cpu", Aggregation: "avg", Color: "blue"}},
	}
}

func TestValidateDashboardPanelDescription(t *testing.T) {
	t.Run("accepts an absent or filled description on every panel type", func(t *testing.T) {
		for _, panelType := range []string{"stat", "timeseries", "table", "text", "row"} {
			panel := descriptionPanel()
			panel.Type = panelType
			if panelType == "text" || panelType == "row" {
				// 文本与行面板不查询数据，校验会拒绝任何查询/可视化字段。
				panel.Series = nil
				panel.Legend = false
			}
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("%s without a description rejected: %v", panelType, err)
			}
			panel.Description = "当前主机的 CPU 使用率"
			if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
				t.Fatalf("%s with a description rejected: %v", panelType, err)
			}
		}
	})

	t.Run("accepts the byte limit exactly", func(t *testing.T) {
		panel := descriptionPanel()
		panel.Description = strings.Repeat("x", MaxPanelDescriptionBytes)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("limit rejected: %v", err)
		}
		// 多字节字符按字节数计数：170 个汉字正好 510 字节。
		panel.Description = strings.Repeat("汉", 170)
		if err := validateDashboardPanel(panel, map[string]bool{}); err != nil {
			t.Fatalf("multi-byte limit rejected: %v", err)
		}
	})

	t.Run("rejects over-long, invalid and NUL descriptions", func(t *testing.T) {
		for _, value := range []string{
			strings.Repeat("x", MaxPanelDescriptionBytes+1),
			strings.Repeat("汉", 171),
			"broken \xff utf8",
			"nul\x00byte",
		} {
			panel := descriptionPanel()
			panel.Description = value
			err := validateDashboardPanel(panel, map[string]bool{})
			if !errors.Is(err, ErrInvalidDashboard) || err.Error() != "invalid metric dashboard: panel description is invalid" {
				t.Fatalf("value %q: unexpected error %v", value, err)
			}
		}
	})
}
