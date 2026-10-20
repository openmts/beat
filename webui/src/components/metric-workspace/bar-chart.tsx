import { useMemo, useState } from "react"
import { Bar, BarChart, CartesianGrid, Rectangle, ReferenceArea, ResponsiveContainer, Tooltip, XAxis } from "recharts"
import { useLocale } from "@/context/locale"
import { chartTimestamp, mergeWorkspaceSeries } from "@/lib/workspace-chart"
import {
  workspaceNoValue, workspaceSeriesAxisPlacement, workspaceSeriesColor, workspaceSeriesDecimals, workspaceSeriesFillOpacity,
  workspaceSeriesName, workspaceSeriesUnit,
} from "@/lib/workspace-overrides"
import { axisHasBothSides, axisSideGroup, type AxisSide } from "@/lib/workspace-axis-sides"
import { gradientFill, panelGradientMode } from "@/lib/workspace-gradient"
import { WorkspaceGradientDefs } from "./gradient-fill"
import { WorkspaceValueAxis } from "./value-axis"
import { WorkspaceLegend } from "./legend"
import { formatWorkspaceValue, workspaceAxisTime } from "@/lib/workspace-format"
import { panelLineStyle } from "@/lib/workspace-line-style"
import { barRadiusCorners, panelBarAlignment, panelBarChart } from "@/lib/workspace-bar-chart"
import { stacksSeries } from "@/lib/workspace-line-style"
import { percentStackGroups, stackedTooltipValueByRef, type StackTotals } from "@/lib/workspace-stack"
import { axisScale, maskLogRows } from "@/lib/workspace-axis-scale"
import { axisGrid, axisPlacement } from "@/lib/workspace-axis-display"
import { legendPlacement } from "@/lib/workspace-legend-placement"
import { dateTimeFormatter } from "@/lib/intl-format"
import type { BarAlignment, DashboardPanel, ExpandedSeries, MetricQueryResult, MetricResultSeries, WorkspaceRange } from "@/types/metric-workspace"

/**
 * Grafana 的 Bar chart：同一份时间序列按柱渲染，一条查询一组柱，堆叠与填充沿用折线的样式字段。
 * 与折线的差异是有意的：柱状图不画阈值线与注释色带（Grafana 的 Bar chart 也没有这两项），
 * 而且取值面板才用的值映射在这里和折线一样被拒绝，避免保存出渲染器会忽略的配置。
 */
export function WorkspaceBarChart(props: {
  panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number
  onRange: (range: WorkspaceRange) => void
}) {
  const { locale, t } = useLocale()
  const style = panelLineStyle(props.panel)
  const bars = panelBarChart(props.panel)
  const corners = barRadiusCorners(bars.radius)
  // auto 与 centered 保持 Recharts 的默认位置（柱子中心落在时间点），只有 before/after 需要自定义柱形。
  const alignment = panelBarAlignment(props.panel)
  const shape = alignment === "before" || alignment === "after" ? alignedBarShape(alignment, corners) : undefined
  const gradient = panelGradientMode(props.panel.gradient_mode)
  const percent = style.stack === "percent"
  const stacked = stacksSeries(style.stack)
  const [hidden, setHidden] = useState<string[]>([])
  const [drag, setDrag] = useState<[number, number] | null>(null)
  // 查询按 max_points 返回定长桶网格（例如 60 秒窗口也会给 600 个桶，其中大多数为空）。
  // 空桶没有柱子，但会各占一个类目：不做处理时 600 个类目会把柱宽压成亚像素细线。
  const logarithmic = axisScale(props.panel) === "log"
  const rows = useMemo(() => {
    const merged = mergeWorkspaceSeries(props.result)
    const filled = merged.filter((row) => props.result.series.some((value) => row[value.ref_id] !== null))
    // 对数刻度下 0 与负数没有位置，与折线一样按缺失样本处理（柱脚贴住轴的行为随之取消）。
    return logarithmic ? maskLogRows(filled, props.result.series.map((value) => value.ref_id)) : filled
  }, [props.result, logarithmic])
  const series = props.result.series.filter((value) => !hidden.includes(value.ref_id))
  const painted = series.map((value, index) => ({ ref_id: value.ref_id, value, side: workspaceSeriesAxisPlacement(props.panel, value),
    color: workspaceSeriesColor(props.panel, value, props.expanded, index),
    fillOpacity: workspaceSeriesFillOpacity(props.panel, value, style.fillOpacity),
    decimals: workspaceSeriesDecimals(props.panel, value) }))
  // tooltip 的行小数位按序列解析；柱状图与折线共用同一个取表口径。
  const decimalsByRef = new Map(painted.map((entry) => [entry.ref_id, entry.decimals]))
  // 双 Y 轴：每条序列按覆盖或面板声明落在左轴或右轴；只有两侧都分到序列时才画第二根轴。
  const sides = useMemo(() => axisSideGroup(props.result.series, (value) => workspaceSeriesAxisPlacement(props.panel, value)),
    [props.result.series, props.panel])
  const dual = axisHasBothSides(sides)
  // 面板声明右轴、但序列全被覆盖挪到左轴时只剩一根轴，轴标签跟着唯一的那根轴走。
  const single: AxisSide = !dual && sides.left.length === 0 && sides.right.length > 0 ? "right" : "left"
  // 百分比堆叠把每个时间点归一化成 100%（双轴时每侧各取满刻度）；tooltip 用还原表继续显示原始值。
  const stackedData = useMemo<{ rows: Record<string, number | null>[]; totals: Map<string, StackTotals> | null }>(() => percent
    ? percentStackGroups(rows, [sides.left.map((value) => value.ref_id), sides.right.map((value) => value.ref_id)])
    : { rows, totals: null }, [rows, percent, sides])
  const chartRows = stackedData.rows, stackTotals = stackedData.totals
  const paintedSides = axisSideGroup(painted, (entry) => entry.side)
  // 堆叠时按同一时间点的柱高之和取刻度，否则柱顶会被画在绘图区之外；每根轴只统计自己那一侧的柱子。
  const maximumOf = (group: typeof painted) => Math.max(0, ...chartRows.map((row) => stacked
    ? group.reduce((total, entry) => total + Math.abs(Number(row[entry.ref_id] ?? 0)), 0)
    : Math.max(0, ...group.map((entry) => Math.abs(Number(row[entry.ref_id] ?? 0))))))
  const unit = props.result.series[0]?.unit ?? "number"
  // 百分比堆叠时坐标轴按百分比刻度显示，tooltip 仍用原始单位与原始值；否则每根轴用它自己第一条序列的单位。
  const axisUnit = (group: MetricResultSeries[]) => percent ? "percent" : group[0] ? workspaceSeriesUnit(props.panel, group[0]) : unit
  const from = Date.parse(props.result.from), to = Date.parse(props.result.to)
  function zoom() {
    if (drag && Math.abs(drag[1] - drag[0]) >= 1000) props.onRange({ mode: "absolute", from: new Date(Math.min(...drag)).toISOString(), to: new Date(Math.max(...drag)).toISOString() })
    setDrag(null)
  }
  // 图例放到右侧时外层改成 flex：ResponsiveContainer 会被压窄、图例保持固定宽度；
  // sr-only 的说明节点是绝对定位，不参与 flex 布局。
  const legendRight = props.panel.legend && legendPlacement(props.panel) === "right"
  return <div className={`min-w-0 ${legendRight ? "flex items-stretch" : ""}`} aria-label={props.panel.title}>
    <ResponsiveContainer width="100%" height={props.height ?? 224}>
      <BarChart data={chartRows} margin={{ top: 12, right: 24, bottom: 8, left: 0 }}
        onMouseDown={(event) => { const value = chartTimestamp(event.activeLabel); if (value !== null) setDrag([value, value]) }}
        onMouseMove={(event) => { const value = chartTimestamp(event.activeLabel); if (drag && value !== null) setDrag([drag[0], value]) }}
        onMouseUp={zoom} onMouseLeave={() => setDrag(null)}>
        {axisGrid(props.panel) && <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />}
        <WorkspaceGradientDefs panelId={props.panel.id} mode={gradient} opacity={style.fillOpacity / 100} direction="up"
          series={painted.map(({ ref_id, color, fillOpacity }) => ({ ref_id, color, opacity: fillOpacity / 100 }))} />
        <XAxis dataKey="timestamp" type="category" allowDataOverflow tickLine={false} axisLine={false} minTickGap={40} fontSize={11}
          tickFormatter={(value) => workspaceAxisTime(Number(value), to - from, locale)} />
        {(dual || single === "left") && <WorkspaceValueAxis side="left" unit={axisUnit(sides.left)} maximum={maximumOf(paintedSides.left)}
          labelled={!dual || axisPlacement(props.panel) === "left"} panel={props.panel} locale={locale} />}
        {(dual || single === "right") && <WorkspaceValueAxis side="right" unit={axisUnit(sides.right)} maximum={maximumOf(paintedSides.right)}
          labelled={!dual || axisPlacement(props.panel) === "right"} panel={props.panel} locale={locale} />}
        {props.panel.tooltip !== "off" && <Tooltip shared={props.panel.tooltip !== "single"} labelFormatter={(label) => `${dateTimeFormatter(locale, { timeZone: "UTC", hour12: false }).format(Number(label))} UTC`} formatter={(value, name, item) => [formatWorkspaceValue(stackedTooltipValueByRef(Number(value), item, stackTotals), unit, { locale, decimals: decimalsByRef.get(barTooltipRefId(item)) ?? props.panel.decimals, noValue: workspaceNoValue(props.panel) }), name]}
          contentStyle={{ background: "var(--popover)", borderColor: "var(--border)", borderRadius: 8, fontSize: 12 }} />}
        {painted.map(({ ref_id, value, side, color, fillOpacity }) => <Bar key={ref_id} name={workspaceSeriesName(props.panel, value, props.expanded)} dataKey={ref_id} yAxisId={side}
          fill={gradientFill(props.panel.id, ref_id, gradient, color)} fillOpacity={gradient === "none" ? fillOpacity / 100 : 1}
          stackId={stacked ? `beat-stack-${side}` : undefined} maxBarSize={bars.width} radius={corners} shape={shape}
          isAnimationActive={false} />)}
        {drag && <ReferenceArea x1={Math.min(...drag)} x2={Math.max(...drag)} fill="var(--foreground)" fillOpacity={0.08} />}
      </BarChart>
    </ResponsiveContainer>
    {props.panel.legend && <WorkspaceLegend panel={props.panel} result={props.result} expanded={props.expanded} hidden={hidden}
      toggle={(refId) => setHidden((current) => current.includes(refId) ? current.filter((id) => id !== refId) : [...current, refId])} />}
    <span className="sr-only">{t("workspace.drag_zoom")}</span>
  </div>
}

/** alignedBarShape 实现 Grafana 的 Bar alignment：before 把柱子整体左移半个柱宽（结束于时间点），
 *  after 右移半个柱宽（起始于时间点）；圆角与填充沿用 Recharts 的柱形语义。 */
function alignedBarShape(alignment: BarAlignment, corners: [number, number, number, number]) {
  return (shapeProps: unknown) => {
    const bar = shapeProps as { x?: number; y?: number; width?: number; height?: number; fill?: string; fillOpacity?: number; stroke?: string }
    const width = Number(bar.width ?? 0)
    const offset = alignment === "before" ? -width / 2 : width / 2
    return <Rectangle x={Number(bar.x ?? 0) + offset} y={Number(bar.y ?? 0)} width={width} height={Number(bar.height ?? 0)}
      radius={corners} fill={bar.fill} fillOpacity={bar.fillOpacity} stroke={bar.stroke} />
  }
}

/** barTooltipRefId 从 recharts 的 tooltip 条目里取序列 ref_id：逐序列小数位按它查表。 */
function barTooltipRefId(item: unknown): string {
  return String((item as { dataKey?: unknown } | undefined)?.dataKey ?? "")
}
