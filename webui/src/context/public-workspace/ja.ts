import type { Catalog } from "@/context/locales"

// 公共节点页会渲染只读指标工作区组件，但公共外壳不加载管理端工作区目录（体积预算）；
// 这里只收录公共路径会显示的 workspace.* 文案，值由 metric-workspace 目录生成，
// context/public-workspace/catalog.test.ts 负责两边一致性。
export const publicWorkspaceJa: Catalog = {
  "ja": {
    "workspace.apply": "適用", "workspace.cancel": "キャンセル", "workspace.create_alert": "アラートルールを作成", "workspace.custom_range": "カスタム範囲",
    "workspace.data": "データ", "workspace.done": "完了", "workspace.drag_zoom": "グラフ上をドラッグして時間方向にズームします。", "workspace.duplicate_panel": "パネルを複製",
    "workspace.duplicate_series": "クエリの参照は一意である必要があります。", "workspace.edit_panel": "パネルを編集", "workspace.export_csv": "CSV をダウンロード", "workspace.export_failed": "ダウンロードに失敗しました。もう一度お試しください。",
    "workspace.export_json": "JSON をダウンロード", "workspace.export_stats_csv": "統計 CSV をダウンロード", "workspace.from": "開始", "workspace.gpu_fixed_node": "GPU デバイスを選択する前に特定のノードを選択してください。", "workspace.heatmap_samples": "サンプル数", "workspace.histogram_samples": "サンプル数", "workspace.state_timeline_states": "状態",
    "workspace.historical": "固定の履歴範囲", "workspace.inspect": "データを検査", "workspace.inspector_limit": "最初の 100 行を表示しています。ダウンロードには全 {rows} 行が含まれます。", "workspace.invalid_panel": "タイトル、クエリ、軸の範囲、しきい値を確認してください。",
    "workspace.invalid_range": "最大 31 日間の有効な時間範囲を指定してください。", "workspace.large": "全幅", "workspace.last15m": "直近 15 分", "workspace.last1h": "直近 1 時間",
    "workspace.last24h": "直近 24 時間", "workspace.last30d": "直近 30 日", "workspace.last5m": "直近 5 分", "workspace.last6h": "直近 6 時間",
    "workspace.last7d": "直近 7 日", "workspace.last_good_range": "新しいクエリが保留中または失敗したため、最後に成功した結果を表示しています。", "workspace.last_success": "最後に成功したクエリ: {time}", "workspace.legend.count": "サンプル数",
    "workspace.legend.last": "最新", "workspace.legend.max": "最大", "workspace.legend.mean": "平均", "workspace.legend.min": "最小",
    "workspace.stats": "統計", "workspace.legend.first": "最初", "workspace.legend.gaps": "欠損",
    "workspace.legend.series": "系列", "workspace.legend.sort": "凡例の並び順", "workspace.legend.sort_asc": "低い順", "workspace.legend.sort_desc": "高い順",
    "workspace.legend.sort_none": "設定どおり", "workspace.legend.sum": "合計", "workspace.legend.tooltip": "ツールチップ", "workspace.legend.tooltip_all": "すべての系列",
    "workspace.legend.tooltip_off": "非表示", "workspace.legend.tooltip_single": "ホバー中の系列", "workspace.legend.values": "凡例の値", "workspace.live": "ライブ範囲",
    "workspace.load_failed": "ワークスペースを読み込めませんでした。", "workspace.medium": "半分の幅", "workspace.metric": "メトリクス", "workspace.move_down": "後ろへ移動",
    "workspace.move_up": "前へ移動", "workspace.no_data": "この範囲にサンプルがありません", "workspace.no_data_description": "より広い範囲を試すか、ノードの収集状態を確認してください。", "workspace.node_drilldown": "このノードに絞り込む",
    "workspace.node_refresh_failed": "ノードの詳細を更新できませんでした。最後に受信した概要を表示しています。", "workspace.node_unavailable": "このノードは公開されていないか、利用できません。", "workspace.off": "オフ", "workspace.pause": "更新を一時停止",
    "workspace.pie_legend": "{title} · スライスの凡例", "workspace.query": "クエリ", "workspace.query_failed": "メトリクスクエリに失敗しました。", "workspace.query_options": "クエリオプション",
    "workspace.refresh": "今すぐ更新", "workspace.refresh_interval": "自動更新", "workspace.remove_panel": "パネルを削除", "workspace.repeat_truncated": "最初の {count} 個の繰り返しパネルのみが描画されます。",
    "workspace.response_summary": "{series} 系列 · {points} ポイント · {interval} 秒バケット", "workspace.retry": "再試行", "workspace.row": "行", "workspace.row_panels": "{count} 個のパネル", "workspace.toggle_row": "行を展開または折りたたむ", "workspace.sample_time": "サンプル取得時刻: {time}", "workspace.seconds": "{seconds} 秒ごと",
    "workspace.geomap_layer_markers": "マーカー", "workspace.geomap_layer_heatmap": "ヒートマップ", "workspace.geomap_no_match": "地点一覧に一致する系列がありません。", "workspace.geomap_unmatched": "{count} 系列に座標がありません",
    "workspace.select_gpu_device": "安定した GPU デバイスを選択してください。", "workspace.select_node": "ノードまたはノードを含むグループを選択してください。", "workspace.series_limit": "1 つのパネルでクエリできる展開後の系列は最大 8 件です。ノードまたはクエリを減らしてください。", "workspace.small": "3 分の 1",
    "workspace.time_range": "時間範囲", "workspace.timestamp": "タイムスタンプ (UTC)", "workspace.to": "終了", "workspace.updating": "更新中…",
    "workspace.utc_hint": "時刻は UTC です。最大範囲は 31 日間です。", "workspace.expression_from": "開始時刻の式", "workspace.expression_to": "終了時刻の式", "workspace.expression_placeholder": "now、now-6h、now-7d、now/d、2026-10-17 09:00", "workspace.expression_invalid": "now、now-6h、now-7d、now/d、または 2026-10-17 09:00（UTC）を入力してください。", "workspace.value": "値", "workspace.variable_picker_hint": "このダッシュボードがクエリできるオプションのみが表示されます。", "workspace.variable_selected": "他 {count} 件",
    "workspace.view_panel": "パネルを表示",
  },
}
