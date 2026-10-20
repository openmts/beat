import type { DashboardPanel, PanelColor, ThresholdDisplayMode } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：off/line/area/background；未声明等于 line（既有看板行为不变）。 */
export const thresholdDisplayModes: readonly ThresholdDisplayMode[] = ["off", "line", "area", "background"]

/** 面板上与阈值显示相关的可编辑子集：编辑器和导入导出都只碰这几个字段。 */
export interface ThresholdDisplayFields {
  type: string
  threshold_mode?: string
  thresholds: { value: number; color: PanelColor }[]
}

/** panelThresholdDisplay 解析生效模式；未知取值同样回落到 line（导入时会被拒绝）。 */
export function panelThresholdDisplay(panel: ThresholdDisplayFields): ThresholdDisplayMode {
  return thresholdDisplayModes.find((mode) => mode === panel.threshold_mode) ?? "line"
}

/** thresholdDisplayError 返回目录键，null 表示可以保存；只有显式声明才约束面板类型。 */
export function thresholdDisplayError(panel: ThresholdDisplayFields): string | null {
  if (panel.threshold_mode === undefined || panel.threshold_mode === "") return null
  if (!thresholdDisplayModes.includes(panel.threshold_mode as ThresholdDisplayMode)) return "workspace.threshold_mode_invalid"
  if (panel.type !== "timeseries" && panel.threshold_mode !== "off") return "workspace.threshold_mode_timeseries_only"
  return null
}

/** thresholdColorDisabled 表示阈值不参与取色：stat/gauge 与折线都回到系列自身颜色。 */
export function thresholdColorDisabled(panel: ThresholdDisplayFields): boolean {
  return panelThresholdDisplay(panel) === "off"
}

/** thresholdRegions 生成 area 模式的色带：每条阈值覆盖它到上一条阈值（或绘图区上沿）之间的区域。 */
export function thresholdRegions(panel: ThresholdDisplayFields): { from: number; to: number | string; color: PanelColor }[] {
  if (panelThresholdDisplay(panel) !== "area") return []
  return panel.thresholds.map((threshold, index) => ({ from: threshold.value, to: panel.thresholds[index + 1]?.value ?? "dataMax", color: threshold.color }))
}

/** thresholdBackground 生成 background 模式背景色：按全部序列最新有效值里的最高值命中最高阈值。 */
export function thresholdBackground(panel: ThresholdDisplayFields, values: (number | null)[]): PanelColor | null {
  if (panelThresholdDisplay(panel) !== "background") return null
  const finite = values.filter((value): value is number => value !== null && Number.isFinite(value))
  if (finite.length === 0 || panel.thresholds.length === 0) return null
  const highest = Math.max(...finite)
  let color: PanelColor | null = null
  for (const threshold of panel.thresholds) if (highest >= threshold.value) color = threshold.color
  return color
}

/** thresholdModePatch 切换面板类型时清掉只对 timeseries 生效的模式，避免保存出服务端会拒绝的组合。 */
export function thresholdModePatch(type: string, mode: string | undefined): Pick<DashboardPanel, "threshold_mode"> {
  const kept = type === "timeseries" || mode === undefined || mode === "" || mode === "off"
  return { threshold_mode: kept ? mode as ThresholdDisplayMode | undefined : undefined }
}
