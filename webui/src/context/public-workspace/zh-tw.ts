import type { Catalog } from "@/context/locales"

// 公共节点页会渲染只读指标工作区组件，但公共外壳不加载管理端工作区目录（体积预算）；
// 这里只收录公共路径会显示的 workspace.* 文案，值由 metric-workspace 目录生成，
// context/public-workspace/catalog.test.ts 负责两边一致性。
export const publicWorkspaceZhTW: Catalog = {
  "zh-TW": {
    "workspace.apply": "套用", "workspace.cancel": "取消", "workspace.create_alert": "建立告警規則", "workspace.custom_range": "自訂範圍",
    "workspace.data": "資料", "workspace.done": "完成", "workspace.drag_zoom": "在圖表上拖曳可縮放時間範圍。", "workspace.duplicate_panel": "複製面板",
    "workspace.duplicate_series": "查詢參照必須唯一。", "workspace.edit_panel": "編輯面板", "workspace.export_csv": "下載 CSV", "workspace.export_failed": "下載失敗，請再試一次。",
    "workspace.export_json": "下載 JSON", "workspace.export_stats_csv": "匯出統計 CSV", "workspace.from": "從", "workspace.gpu_fixed_node": "選擇 GPU 裝置前請先指定特定節點。", "workspace.heatmap_samples": "樣本數", "workspace.histogram_samples": "樣本數", "workspace.state_timeline_states": "狀態",
    "workspace.historical": "固定的歷史範圍", "workspace.inspect": "檢視資料", "workspace.inspector_limit": "僅顯示前 100 列。下載會包含全部 {rows} 列。", "workspace.invalid_panel": "請檢查標題、查詢、軸邊界與閾值。",
    "workspace.invalid_range": "請選擇有效的時間範圍，最長 31 天。", "workspace.large": "全寬", "workspace.last15m": "最近 15 分鐘", "workspace.last1h": "最近 1 小時",
    "workspace.last24h": "最近 24 小時", "workspace.last30d": "最近 30 天", "workspace.last5m": "最近 5 分鐘", "workspace.last6h": "最近 6 小時",
    "workspace.last7d": "最近 7 天", "workspace.last_good_range": "新查詢仍在等待或失敗時，顯示最近一次成功的結果。", "workspace.last_success": "最近成功的查詢：{time}", "workspace.legend.count": "樣本數",
    "workspace.legend.last": "最新", "workspace.legend.max": "最大", "workspace.legend.mean": "平均", "workspace.legend.min": "最小",
    "workspace.stats": "統計", "workspace.legend.first": "首個", "workspace.legend.gaps": "空桶",
    "workspace.legend.series": "序列", "workspace.legend.sort": "圖例排序", "workspace.legend.sort_asc": "由低到高", "workspace.legend.sort_desc": "由高到低",
    "workspace.legend.sort_none": "依設定", "workspace.legend.sum": "合計", "workspace.legend.tooltip": "工具提示", "workspace.legend.tooltip_all": "所有序列",
    "workspace.legend.tooltip_off": "隱藏", "workspace.legend.tooltip_single": "游標所在序列", "workspace.legend.values": "圖例值", "workspace.live": "即時範圍",
    "workspace.load_failed": "工作區載入失敗。", "workspace.medium": "一半寬度", "workspace.metric": "指標", "workspace.move_down": "往後移動",
    "workspace.move_up": "往前移動", "workspace.no_data": "此範圍內沒有樣本", "workspace.no_data_description": "請嘗試更寬的範圍，或檢查節點的收集狀態。", "workspace.node_drilldown": "聚焦此節點",
    "workspace.node_refresh_failed": "無法重新整理節點詳細資料，顯示最近收到的摘要。", "workspace.node_unavailable": "此節點已不再公開或不可用。", "workspace.off": "關閉", "workspace.pause": "暫停重新整理",
    "workspace.pie_legend": "{title} · 扇區圖例", "workspace.query": "查詢", "workspace.query_failed": "指標查詢失敗。", "workspace.query_options": "查詢選項",
    "workspace.refresh": "立即重新整理", "workspace.refresh_interval": "自動重新整理", "workspace.remove_panel": "移除面板", "workspace.repeat_truncated": "僅繪製前 {count} 個重複面板。",
    "workspace.response_summary": "{series} 條序列 · {points} 個點 · {interval} 秒分桶", "workspace.retry": "重試", "workspace.row": "列", "workspace.row_panels": "{count} 個面板", "workspace.toggle_row": "展開或摺疊此列", "workspace.sample_time": "樣本擷取時間：{time}", "workspace.seconds": "每 {seconds} 秒",
    "workspace.geomap_layer_markers": "標記", "workspace.geomap_layer_heatmap": "熱力", "workspace.geomap_no_match": "沒有序列比對到地點表中的地點。", "workspace.geomap_unmatched": "{count} 條序列缺少座標",
    "workspace.select_gpu_device": "請選擇穩定的 GPU 裝置。", "workspace.select_node": "請選擇一個節點或含有節點的群組。", "workspace.series_limit": "每個面板最多可查詢八條展開後的序列。請減少節點或查詢。", "workspace.small": "三分之一",
    "workspace.time_range": "時間範圍", "workspace.timestamp": "時間戳（UTC）", "workspace.to": "到", "workspace.updating": "更新中…",
    "workspace.utc_hint": "時間以 UTC 顯示。範圍上限為 31 天。", "workspace.expression_from": "開始時間表達式", "workspace.expression_to": "結束時間表達式", "workspace.expression_placeholder": "now、now-6h、now-7d、now/d、2026-10-17 09:00", "workspace.expression_invalid": "請使用 now、now-6h、now-7d、now/d 或 2026-10-17 09:00（UTC）。", "workspace.value": "值", "workspace.variable_picker_hint": "只會列出此儀表板可查詢的選項。", "workspace.variable_selected": "另有 {count} 個",
    "workspace.view_panel": "檢視面板",
  },
}
