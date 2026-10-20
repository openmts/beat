// Stat 面板的迷你趋势图（Grafana 的 Value options → Sparkline）：把同一条序列压成两步就能读完的折线。
// 这里只做纯几何计算，颜色与数值格式化仍由渲染器负责，便于在测试里直接断言路径数据。

import { gradientFillOpacity, gradientModes, type GradientMode } from "@/lib/workspace-line-style"
import type { DashboardPanel, MetricResultSeries } from "@/types/metric-workspace"

/** 迷你趋势图的默认画布尺寸（viewBox 单位，实际显示宽度由 CSS 决定）。 */
export const sparklineWidth = 160
export const sparklineHeight = 32

/** 线宽与填充的取值范围与服务端 validateDashboardSparkline 保持一致。 */
export const minSparklineLineWidth = 1
export const maxSparklineLineWidth = 4
export const defaultSparklineLineWidth = 2
export const maxSparklineFillOpacity = 100

type Coord = readonly [number, number]

/** 迷你趋势图的输入面：校验与导入只需要这四项，调用方不必造出整块面板。 */
export type SparklineFields = Pick<DashboardPanel, "type" | "sparkline" | "sparkline_line_width" | "sparkline_fill_opacity" | "sparkline_gradient_mode">

export function panelSparkline(panel: Pick<DashboardPanel, "sparkline">): boolean {
  return panel.sparkline === true
}

/** sparklineStyle 把面板配置归一成渲染参数：越界或缺省都退回默认值，渲染器不需要再判断。 */
export function sparklineStyle(panel: SparklineFields): { lineWidth: number; fillOpacity: number; gradientMode: GradientMode } {
  const width = panel.sparkline_line_width
  const fill = panel.sparkline_fill_opacity
  return {
    lineWidth: Number.isInteger(width) && width! >= minSparklineLineWidth && width! <= maxSparklineLineWidth ? width! : defaultSparklineLineWidth,
    fillOpacity: typeof fill === "number" && Number.isFinite(fill) && fill >= 0 && fill <= maxSparklineFillOpacity ? fill : 0,
    gradientMode: gradientModes.find((mode) => mode === panel.sparkline_gradient_mode) ?? "none",
  }
}

/** sparklineError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardSparkline 一致。 */
export function sparklineError(panel: SparklineFields): string | null {
  const options = panel.sparkline_line_width !== undefined || panel.sparkline_fill_opacity !== undefined ||
    panel.sparkline_gradient_mode !== undefined
  if (panel.sparkline === undefined && !options) return null
  if (panel.type !== "stat") return "workspace.sparkline_stat_only"
  if (!panelSparkline(panel)) return options ? "workspace.sparkline_options_require_on" : null
  if (panel.sparkline_line_width !== undefined && !validSparklineWidth(panel.sparkline_line_width)) return "workspace.line_width_invalid"
  if (panel.sparkline_fill_opacity !== undefined && !validSparklineFill(panel.sparkline_fill_opacity)) return "workspace.fill_opacity_invalid"
  if (panel.sparkline_gradient_mode !== undefined && !gradientModes.some((mode) => mode === panel.sparkline_gradient_mode)) return "workspace.gradient_mode_invalid"
  // 与服务端 validateDashboardSparkline 同规则：渐变必须有可渐变的填充，否则「选了但看不出效果」。
  const style = sparklineStyle(panel)
  if (style.gradientMode !== "none" && style.fillOpacity === 0) return "workspace.gradient_requires_fill"
  return null
}

function validSparklineWidth(value: number): boolean {
  return Number.isInteger(value) && value >= minSparklineLineWidth && value <= maxSparklineLineWidth
}

function validSparklineFill(value: number): boolean {
  return Number.isFinite(value) && value >= 0 && value <= maxSparklineFillOpacity
}

/** sparklinePatch 切换面板类型时清理只对 stat 有效的开关与样式，避免保存出服务端会拒绝的组合。 */
export function sparklinePatch(type: string): Pick<DashboardPanel, "sparkline" | "sparkline_line_width" | "sparkline_fill_opacity" | "sparkline_gradient_mode"> {
  return type === "stat" ? {} : { sparkline: undefined, sparkline_line_width: undefined, sparkline_fill_opacity: undefined, sparkline_gradient_mode: undefined }
}

/** sparklineGradientPatch 与折线的 gradientPatch 同规则：打开渐变时若还没有填充，补一个可读的默认值，
 *  避免保存出「选了渐变但填充是 0」的无效组合；选回 none 时省略字段。 */
export function sparklineGradientPatch(panel: SparklineFields, mode: string): Pick<DashboardPanel, "sparkline_gradient_mode" | "sparkline_fill_opacity"> {
  const gradient = gradientModes.find((value) => value === mode)
  if (gradient === undefined || gradient === "none") return { sparkline_gradient_mode: undefined }
  return sparklineStyle(panel).fillOpacity > 0
    ? { sparkline_gradient_mode: gradient }
    : { sparkline_gradient_mode: gradient, sparkline_fill_opacity: gradientFillOpacity }
}

/** sparklineSegments 生成 SVG path：空值断开折线，只保留至少两个点的连续段。
 *  整条序列取值相同时画在中线上，避免零除；点不足两个时返回空数组，渲染器不画图形。 */
export function sparklineSegments(
  points: MetricResultSeries["points"], width = sparklineWidth, height = sparklineHeight,
): string[] {
  return sparklineCoords(points, width, height).map(sparklinePath)
}

/** sparklineAreaPaths 生成每段折线下方的闭合填充路径：右端落到基线，再沿基线回到起点并闭合。 */
export function sparklineAreaPaths(
  points: MetricResultSeries["points"], width = sparklineWidth, height = sparklineHeight,
): string[] {
  return sparklineCoords(points, width, height).map((coords) => {
    const first = coords[0], last = coords[coords.length - 1]
    return `${sparklinePath(coords)} L ${last[0].toFixed(2)} ${height.toFixed(2)} L ${first[0].toFixed(2)} ${height.toFixed(2)} Z`
  })
}

function sparklineCoords(points: MetricResultSeries["points"], width: number, height: number): Coord[][] {
  const values = points.flatMap((point) => (point.value === null ? [] : [point.value]))
  if (values.length < 2) return []
  const minimum = Math.min(...values), maximum = Math.max(...values), span = maximum - minimum
  const axis = sparklineAxis(points, width)
  const segments: Coord[][] = []
  let current: Coord[] = []
  points.forEach((point, index) => {
    if (point.value === null) {
      if (current.length > 1) segments.push(current)
      current = []
      return
    }
    const ratio = span === 0 ? 0.5 : (point.value - minimum) / span
    current.push([axis(index, point.timestamp), height - ratio * height])
  })
  if (current.length > 1) segments.push(current)
  return segments
}

/** sparklineAxis 把采样点映射到横轴：与 Grafana 一致按真实时间定位，因此不匀采样会显示成不匀的间距。
 *  时间缺失、非有限值或整段同一时刻时退回等分索引，保证旧数据仍能画出完整折线。 */
function sparklineAxis(points: MetricResultSeries["points"], width: number): (index: number, timestamp: number) => number {
  const first = points[0]?.timestamp, last = points[points.length - 1]?.timestamp
  const usable = points.length > 1 &&
    Number.isFinite(first) && Number.isFinite(last) &&
    points.every((point) => Number.isFinite(point.timestamp)) &&
    last - first > 0
  if (!usable) {
    const step = width / Math.max(1, points.length - 1)
    return (index) => index * step
  }
  const timeSpan = last - first
  return (_index, timestamp) => ((timestamp - first) / timeSpan) * width
}

function sparklinePath(coords: Coord[]): string {
  return `M ${coords.map(([x, y]) => `${x.toFixed(2)} ${y.toFixed(2)}`).join(" L ")}`
}
