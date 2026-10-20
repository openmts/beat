import { seriesLegendStatistics } from "./workspace-chart"
import type { DashboardPanel, MetricResultSeries, PanelCalculation, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：last/first/min/max/mean/sum/count/range/delta/percent_change；
 *  未声明等于 last（最新有效样本）。派生取值对应 Grafana 的 Range/Delta/Percent change。 */
export const panelCalculations: readonly PanelCalculation[] = [
  "last", "first", "min", "max", "mean", "sum", "count", "range", "delta", "percent_change",
]

/** 把一条序列显示成一个数的面板类型；折线保留采样点、表格保留原始行，都不接受该字段。 */
export const panelCalculationTypes: readonly PanelType[] = ["stat", "gauge", "bargauge", "piechart"]

/** 面板上与值计算相关的可编辑子集：编辑器与导入导出都只碰这个字段。 */
export interface CalculationFields {
  type: string
  calculation?: string
}

/** panelCalculation 解析生效的计算方式；未知取值同样回落到 last（导入时会被拒绝）。 */
export function panelCalculation(panel: CalculationFields): PanelCalculation {
  return panelCalculations.find((value) => value === panel.calculation) ?? "last"
}

/** calculationError 返回目录键，null 表示可以保存；只有显式声明才约束面板类型。 */
export function calculationError(panel: CalculationFields): string | null {
  if (panel.calculation === undefined || panel.calculation === "") return null
  if (!panelCalculations.includes(panel.calculation as PanelCalculation)) return "workspace.calculation_invalid"
  if (!panelCalculationTypes.includes(panel.type as PanelType)) return "workspace.calculation_value_panels_only"
  return null
}

/** calculationPatch 切换面板类型时清掉新类型渲染不了的取值，避免保存出服务端会拒绝的组合。 */
export function calculationPatch(type: string, calculation: string | undefined): Pick<DashboardPanel, "calculation"> {
  const kept = panelCalculationTypes.includes(type as PanelType)
  return { calculation: kept ? calculation as PanelCalculation | undefined : undefined }
}

/**
 * panelCalculationValue 按面板的计算方式把采样序列压成一个数值。
 * 统计量在没有有效样本时统一为 null，因此 sum 不会把空序列显示成 0、
 * count 也不会把空序列显示成「0 个样本」，渲染层据此走「无数据」。
 */
export function panelCalculationValue(series: MetricResultSeries, calculation: PanelCalculation): number | null {
  if (calculation === "last" || calculation === "min" || calculation === "max" ||
    calculation === "mean" || calculation === "sum" || calculation === "count") {
    return seriesLegendStatistics(series)[calculation]
  }
  return windowCalculationValue(series, calculation)
}

/** windowCalculationValue 实现需要按窗口首末样本推导的取值：first、range（极差）、
 *  delta（累计变化）与 percent_change（百分比变化）；没有可比基准时返回 null，让渲染层
 *  走「无数据」，而不是伪造一个 0。 */
function windowCalculationValue(series: MetricResultSeries, calculation: PanelCalculation): number | null {
  const stats = panelWindowStats(series)
  if (calculation === "first") return stats.first
  if (stats.count === 0 || stats.min === null || stats.max === null) return null
  if (calculation === "range") return stats.max - stats.min
  if (stats.count < 2 || stats.first === null || stats.last === null) return null
  if (calculation === "delta") return stats.last - stats.first
  return stats.first === 0 ? null : (stats.last - stats.first) / Math.abs(stats.first) * 100
}

/** panelWindowStats 用单次遍历给出首末样本与极值，供 windowCalculationValue 使用。 */
function panelWindowStats(series: MetricResultSeries) {
  let count = 0, first: number | null = null, last: number | null = null
  let lowest: number | null = null, highest: number | null = null
  for (const point of series.points) {
    const value = point.value
    if (value === null || !Number.isFinite(value)) continue
    if (first === null) first = value
    last = value
    lowest = lowest === null ? value : Math.min(lowest, value)
    highest = highest === null ? value : Math.max(highest, value)
    count += 1
  }
  return { count, first, last, min: lowest, max: highest }
}
