import type { Catalog } from "@/context/locales"

// 公共节点页会渲染只读指标工作区组件，但公共外壳不加载管理端工作区目录（体积预算）；
// 这里只收录公共路径会显示的 workspace.* 文案，值由 metric-workspace 目录生成，
// context/public-workspace/catalog.test.ts 负责两边一致性。
export const publicWorkspaceEn: Catalog = {
  "en": {
    "workspace.apply": "Apply", "workspace.cancel": "Cancel", "workspace.create_alert": "Create alert rule", "workspace.custom_range": "Custom range",
    "workspace.data": "Data", "workspace.done": "Done", "workspace.drag_zoom": "Drag across a chart to zoom into time.", "workspace.duplicate_panel": "Duplicate panel",
    "workspace.duplicate_series": "Query references must be unique.", "workspace.edit_panel": "Edit panel", "workspace.export_csv": "Download CSV", "workspace.export_failed": "Download failed. Try again.",
    "workspace.export_json": "Download JSON", "workspace.export_stats_csv": "Download stats CSV", "workspace.from": "From", "workspace.gpu_fixed_node": "Choose a specific node before selecting a GPU device.", "workspace.heatmap_samples": "Samples", "workspace.histogram_samples": "Samples", "workspace.state_timeline_states": "States",
    "workspace.historical": "Fixed historical range", "workspace.inspect": "Inspect data", "workspace.inspector_limit": "Showing the first 100 rows. Downloads include all {rows} rows.", "workspace.invalid_panel": "Check the title, queries, axis bounds and thresholds.",
    "workspace.invalid_range": "Choose a valid time range of up to 31 days.", "workspace.large": "Full width", "workspace.last15m": "Last 15 minutes", "workspace.last1h": "Last hour",
    "workspace.last24h": "Last 24 hours", "workspace.last30d": "Last 30 days", "workspace.last5m": "Last 5 minutes", "workspace.last6h": "Last 6 hours",
    "workspace.last7d": "Last 7 days", "workspace.last_good_range": "Showing the last successful result while the new query is pending or failed.", "workspace.last_success": "Last successful query: {time}", "workspace.legend.count": "Count",
    "workspace.legend.last": "Last", "workspace.legend.max": "Max", "workspace.legend.mean": "Mean", "workspace.legend.min": "Min",
    "workspace.stats": "Stats", "workspace.legend.first": "First", "workspace.legend.gaps": "Gaps",
    "workspace.legend.series": "Series", "workspace.legend.sort": "Legend order", "workspace.legend.sort_asc": "Lowest first", "workspace.legend.sort_desc": "Highest first",
    "workspace.legend.sort_none": "As configured", "workspace.legend.sum": "Sum", "workspace.legend.tooltip": "Tooltip", "workspace.legend.tooltip_all": "All series",
    "workspace.legend.tooltip_off": "Hidden", "workspace.legend.tooltip_single": "Hovered series", "workspace.legend.values": "Legend values", "workspace.live": "Live range",
    "workspace.load_failed": "The workspace could not be loaded.", "workspace.medium": "Half width", "workspace.metric": "Metric", "workspace.move_down": "Move later",
    "workspace.move_up": "Move earlier", "workspace.no_data": "No samples in this range", "workspace.no_data_description": "Try a wider range or check the node's collection status.", "workspace.node_drilldown": "Focus this node",
    "workspace.node_refresh_failed": "Node details could not be refreshed. Showing the last received summary.", "workspace.node_unavailable": "This node is no longer public or available.", "workspace.off": "Off", "workspace.pause": "Pause refresh",
    "workspace.pie_legend": "{title} · slice legend", "workspace.query": "Query", "workspace.query_failed": "The metric query failed.", "workspace.query_options": "Query options",
    "workspace.refresh": "Refresh now", "workspace.refresh_interval": "Auto refresh", "workspace.remove_panel": "Remove panel", "workspace.repeat_truncated": "Only the first {count} repeated panels are rendered.",
    "workspace.response_summary": "{series} series · {points} points · {interval}s buckets", "workspace.retry": "Retry", "workspace.row": "Row", "workspace.row_panels": "{count} panels", "workspace.toggle_row": "Expand or collapse this row", "workspace.sample_time": "Sample captured: {time}", "workspace.seconds": "Every {seconds}s",
    "workspace.geomap_layer_markers": "Markers", "workspace.geomap_layer_heatmap": "Heatmap", "workspace.geomap_no_match": "No series matched a location in the location table.", "workspace.geomap_unmatched": "{count} series without coordinates",
    "workspace.select_gpu_device": "Select a stable GPU device.", "workspace.select_node": "Select a node or a group with nodes.", "workspace.series_limit": "A panel can query up to eight expanded series. Select fewer nodes or queries.", "workspace.small": "One third",
    "workspace.time_range": "Time range", "workspace.timestamp": "Timestamp (UTC)", "workspace.to": "To", "workspace.updating": "Updating…",
    "workspace.utc_hint": "Times are in UTC. Maximum range: 31 days.", "workspace.expression_from": "From expression", "workspace.expression_to": "To expression", "workspace.expression_placeholder": "now, now-6h, now-7d, now/d, 2026-10-17 09:00", "workspace.expression_invalid": "Use now, now-6h, now-7d, now/d or 2026-10-17 09:00 (UTC).", "workspace.value": "Value", "workspace.variable_picker_hint": "Only options this dashboard can query are listed.", "workspace.variable_selected": "+{count} more",
    "workspace.view_panel": "View panel",
  },
}
