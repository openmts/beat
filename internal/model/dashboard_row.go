package model

import "fmt"

// Row 面板对齐 Grafana 的行（Row）：本身不查询数据，只把其后的面板归到同一行，并记录默认折叠状态。
// 行面板因此只携带标题、尺寸与 row_collapsed；任何查询或可视化选项都必须留空，避免保存出
// 「配置写了但永远不渲染」的看板。
func validateDashboardRow(panel DashboardPanel) error {
	if panel.Type != "row" {
		if panel.RowCollapsed != nil {
			return fmt.Errorf("%w: row collapse is only available on row panels", ErrInvalidDashboard)
		}
		return nil
	}
	if rowCarriesPanelOptions(panel) {
		return fmt.Errorf("%w: row panels only carry a title, size and collapse state", ErrInvalidDashboard)
	}
	return nil
}

// rowCarriesPanelOptions 列出所有对行面板无意义的字段。行不查询数据、不渲染图形与图例，
// 因此这些字段一旦出现在行上就是配置错误；数据链接同样不在行上渲染。
func rowCarriesPanelOptions(panel DashboardPanel) bool {
	return panelCarriesQueryOptions(panel) || len(panel.Links) != 0
}

// panelCarriesQueryOptions 列出「不查询数据的面板」（行与文本面板）禁止携带的字段。
// 新增面板字段时也要在这里登记，否则会出现「配置写了但永远不渲染」的看板。
func panelCarriesQueryOptions(panel DashboardPanel) bool {
	return len(panel.Series) != 0 || panel.Repeat != "" || panel.Legend ||
		len(panel.LegendValues) != 0 || panel.LegendSort != "" || panel.LegendWidth != nil || panel.Tooltip != "" ||
		panel.TimeFrom != "" || panel.TimeTo != "" || panel.TimeShift != "" || panel.MaxPoints != nil ||
		panel.LineWidth != nil || panel.FillOpacity != nil || panel.ShowPoints != "" || panel.Stack != "" ||
		panel.BarOrientation != "" || panel.BarDisplay != "" || panel.BarUnfilled != nil ||
		panel.BarWidth != nil || panel.BarRadius != nil || panel.BarAlignment != "" ||
		panel.PieChartType != "" || panel.PieLabelMode != "" ||
		panel.HeatmapBuckets != nil || panel.HeatmapScale != "" || panel.HeatmapColor != "" ||
		panel.HistogramBuckets != nil || panel.HistogramScale != "" || panel.HistogramColor != "" ||
		panel.HistogramMerge != nil || panel.TimelineMerge != nil || panel.TimelineShowValues != "" ||
		len(panel.GeoLocations) != 0 || panel.GeoLayer != "" || panel.GeoTileURL != "" || panel.GeoZoom != nil ||
		panel.GeoCenterLatitude != nil || panel.GeoCenterLongitude != nil || panel.GeoMarkerSize != nil ||
		panel.Unit != "" || panel.Decimals != nil || panel.Min != nil || panel.Max != nil ||
		len(panel.Thresholds) != 0 || panel.ThresholdMode != "" || panel.Calculation != "" || panel.Sparkline != nil ||
		panel.SparklineLineWidth != nil || panel.SparklineFillOpacity != nil ||
		panel.GaugeThresholdMarkers != nil || panel.GaugeThresholdLabels != nil ||
		len(panel.ValueMappings) != 0 || len(panel.Transformations) != 0 || len(panel.Overrides) != 0
}
