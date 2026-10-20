import type { DashboardPanel, PanelColor } from "@/types/metric-workspace"

/** 阈值刻度的默认值对齐 Grafana Gauge：刻度显示、标签不显示。 */
export function panelGaugeThresholds(panel: { gauge_threshold_markers?: boolean; gauge_threshold_labels?: boolean }): { markers: boolean; labels: boolean } {
  return { markers: panel.gauge_threshold_markers ?? true, labels: panel.gauge_threshold_labels ?? false }
}

/** gaugeThresholdError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardGaugeThresholds 一致。 */
export function gaugeThresholdError(panel: { type: string; gauge_threshold_markers?: boolean; gauge_threshold_labels?: boolean; thresholds: { value: number }[] }): string | null {
  const markers = panel.gauge_threshold_markers, labels = panel.gauge_threshold_labels
  if (markers === undefined && labels === undefined) return null
  if (panel.type !== "gauge") return "workspace.gauge_threshold_gauge_only"
  if (labels === true && markers === false) return "workspace.gauge_threshold_labels_need_markers"
  if (labels === true && panel.thresholds.length === 0) return "workspace.gauge_threshold_labels_need_thresholds"
  return null
}

/** gaugeThresholdPatch 切到非 gauge 面板时清掉两个字段，避免保存出服务端会拒绝的组合。 */
export function gaugeThresholdPatch(type: string): Pick<DashboardPanel, "gauge_threshold_markers" | "gauge_threshold_labels"> {
  return type === "gauge" ? {} : { gauge_threshold_markers: undefined, gauge_threshold_labels: undefined }
}

/** gaugeThresholdToggle 把开关归一成补丁：默认值直接省略字段，配置文件里只留非默认项。 */
export function gaugeThresholdToggle(key: "gauge_threshold_markers" | "gauge_threshold_labels", value: boolean): Pick<DashboardPanel, "gauge_threshold_markers" | "gauge_threshold_labels"> {
  const fallback = key === "gauge_threshold_markers"
  return value === fallback ? { [key]: undefined } : { [key]: value }
}

/** gaugeThresholdMarks 计算阈值在仪表条上的落点：只保留落在轴范围内的阈值，比例落在 0..1。 */
export function gaugeThresholdMarks(
  panel: Pick<DashboardPanel, "thresholds">, minimum: number, maximum: number,
): { value: number; color: PanelColor; ratio: number }[] {
  const span = maximum - minimum
  if (!Number.isFinite(span) || span <= 0) return []
  return panel.thresholds
    .map((threshold) => ({ value: threshold.value, color: threshold.color, ratio: (threshold.value - minimum) / span }))
    .filter((mark) => mark.ratio >= 0 && mark.ratio <= 1)
}
