import type { Catalog } from "@/context/locales"

// 公共节点页会渲染只读指标工作区组件，但公共外壳不加载管理端工作区目录（体积预算）；
// 这里只收录公共路径会显示的 workspace.* 文案，值由 metric-workspace 目录生成，
// context/public-workspace/catalog.test.ts 负责两边一致性。
export const publicWorkspaceZhCN: Catalog = {
  "zh-CN": {
    "workspace.apply": "应用", "workspace.cancel": "取消", "workspace.create_alert": "创建告警规则", "workspace.custom_range": "自定义时间",
    "workspace.data": "数据", "workspace.done": "完成", "workspace.drag_zoom": "在图表上拖动可缩小时间范围。", "workspace.duplicate_panel": "复制面板",
    "workspace.duplicate_series": "查询标识不能重复。", "workspace.edit_panel": "编辑面板", "workspace.export_csv": "下载 CSV", "workspace.export_failed": "下载失败，请重试。",
    "workspace.export_json": "下载 JSON", "workspace.export_stats_csv": "导出统计 CSV", "workspace.from": "开始时间", "workspace.gpu_fixed_node": "请先选择具体节点，再选择 GPU 设备。", "workspace.heatmap_samples": "样本数", "workspace.histogram_samples": "样本数", "workspace.state_timeline_states": "状态",
    "workspace.historical": "固定历史范围", "workspace.inspect": "检查数据", "workspace.inspector_limit": "显示前 100 行，下载包含全部 {rows} 行。", "workspace.invalid_panel": "请检查标题、查询、坐标轴范围和阈值。",
    "workspace.invalid_range": "请选择有效时间范围，最长为 31 天。", "workspace.large": "全宽", "workspace.last15m": "最近 15 分钟", "workspace.last1h": "最近 1 小时",
    "workspace.last24h": "最近 24 小时", "workspace.last30d": "最近 30 天", "workspace.last5m": "最近 5 分钟", "workspace.last6h": "最近 6 小时",
    "workspace.last7d": "最近 7 天", "workspace.last_good_range": "新查询仍在进行或已失败，目前显示上次成功的结果。", "workspace.last_success": "上次成功查询：{time}", "workspace.legend.count": "样本数",
    "workspace.legend.last": "最新", "workspace.legend.max": "最大", "workspace.legend.mean": "平均", "workspace.legend.min": "最小",
    "workspace.stats": "统计", "workspace.legend.first": "首个", "workspace.legend.gaps": "空桶",
    "workspace.legend.series": "序列", "workspace.legend.sort": "图例排序", "workspace.legend.sort_asc": "数值升序", "workspace.legend.sort_desc": "数值降序",
    "workspace.legend.sort_none": "按配置顺序", "workspace.legend.sum": "合计", "workspace.legend.tooltip": "提示框", "workspace.legend.tooltip_all": "全部序列",
    "workspace.legend.tooltip_off": "隐藏", "workspace.legend.tooltip_single": "当前序列", "workspace.legend.values": "图例取值", "workspace.live": "实时范围",
    "workspace.load_failed": "无法加载指标工作区。", "workspace.medium": "半宽", "workspace.metric": "指标", "workspace.move_down": "向后移动",
    "workspace.move_up": "向前移动", "workspace.no_data": "此时间范围内没有样本", "workspace.no_data_description": "请扩大时间范围，或检查节点的采集状态。", "workspace.node_drilldown": "聚焦此节点",
    "workspace.node_refresh_failed": "节点信息刷新失败，当前显示上次收到的摘要。", "workspace.node_unavailable": "此节点已取消公开或不可用。", "workspace.off": "关闭", "workspace.pause": "暂停刷新",
    "workspace.pie_legend": "{title} · 扇区图例", "workspace.query": "查询", "workspace.query_failed": "指标查询失败。", "workspace.query_options": "查询选项",
    "workspace.refresh": "立即刷新", "workspace.refresh_interval": "自动刷新", "workspace.remove_panel": "移除面板", "workspace.repeat_truncated": "只渲染前 {count} 个重复面板。",
    "workspace.response_summary": "{series} 条序列 · {points} 个点 · 每 {interval} 秒一组", "workspace.retry": "重试", "workspace.row": "行", "workspace.row_panels": "{count} 个面板", "workspace.toggle_row": "展开或折叠该行", "workspace.sample_time": "样本采集时间：{time}", "workspace.seconds": "每 {seconds} 秒",
    "workspace.geomap_layer_markers": "标记", "workspace.geomap_layer_heatmap": "热力", "workspace.geomap_no_match": "没有序列匹配到地点表中的地点。", "workspace.geomap_unmatched": "{count} 条序列缺少坐标",
    "workspace.select_gpu_device": "请选择稳定的 GPU 设备。", "workspace.select_node": "请选择节点或包含节点的分组。", "workspace.series_limit": "每个面板最多查询 8 条序列，请减少节点或查询数量。", "workspace.small": "三分之一",
    "workspace.time_range": "时间范围", "workspace.timestamp": "时间（UTC）", "workspace.to": "结束时间", "workspace.updating": "正在更新…",
    "workspace.utc_hint": "时间均为 UTC，最多查询 31 天。", "workspace.expression_from": "开始时间表达式", "workspace.expression_to": "结束时间表达式", "workspace.expression_placeholder": "now、now-6h、now-7d、now/d、2026-10-17 09:00", "workspace.expression_invalid": "请使用 now、now-6h、now-7d、now/d 或 2026-10-17 09:00（UTC）。", "workspace.value": "数值", "workspace.variable_picker_hint": "只列出该看板可以查询的选项。", "workspace.variable_selected": "另有 {count} 项",
    "workspace.view_panel": "放大查看",
  },
}
