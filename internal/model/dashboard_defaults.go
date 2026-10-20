package model

// DefaultMetricDashboard 不含固定节点或私有指标，可供匿名页面安全使用。
func DefaultMetricDashboard(scope string) MetricDashboard {
	id := PublicDashboardID
	if scope == "admin" {
		id = AdminDashboardID
	}
	return MetricDashboard{ID: id, Scope: scope, Definition: DashboardDefinition{
		Version: DashboardVersion, Title: "Node overview", TimeRangeSeconds: 3600, RefreshSeconds: 15,
		Variables: []DashboardVariable{{Name: "node", Type: "node", Label: "Node"}},
		Panels: []DashboardPanel{
			defaultDashboardPanel("cpu", "CPU usage", "blue"),
			defaultDashboardPanel("memory", "Memory usage", "violet"),
			defaultDashboardPanel("disk", "Disk usage", "amber"),
			defaultNetworkPanel(),
		},
	}}
}

func defaultDashboardPanel(metric, title, color string) DashboardPanel {
	return DashboardPanel{ID: metric, Title: title, Type: "timeseries", Size: "medium", Legend: true, Unit: "auto",
		Thresholds: []DashboardThreshold{}, Series: []DashboardSeries{{
			RefID: "A", NodeID: "$node", Metric: metric, Aggregation: "avg", Label: title, Color: color,
		}}}
}

func defaultNetworkPanel() DashboardPanel {
	panel := defaultDashboardPanel("net_recv", "Network transfer", "cyan")
	panel.Series[0].Label = "Received"
	panel.Series = append(panel.Series, DashboardSeries{
		RefID: "B", NodeID: "$node", Metric: "net_sent", Aggregation: "avg", Label: "Sent", Color: "green",
	})
	return panel
}
