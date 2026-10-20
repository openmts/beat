import { Fragment, useMemo, useState } from "react"
import { Area, CartesianGrid, ComposedChart, Line, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis } from "recharts"
import { useLocale } from "@/context/locale"
import { chartTimestamp, latestWorkspaceValue, mergeWorkspaceSeries, panelColors } from "@/lib/workspace-chart"
import {
  workspaceNoValue, workspaceSeriesAxisPlacement, workspaceSeriesColor, workspaceSeriesDecimals,
  workspaceSeriesFillOpacity, workspaceSeriesLineWidth, workspaceSeriesName, workspaceSeriesUnit, workspaceSideAxisOptions,
} from "@/lib/workspace-overrides"
import { axisHasBothSides, axisSideGroup, axisSideMaximum, type AxisSide } from "@/lib/workspace-axis-sides"
import { gradientFill, panelGradientMode } from "@/lib/workspace-gradient"
import { linePatternDash } from "@/lib/workspace-line-style"
import { legendPlacement } from "@/lib/workspace-legend-placement"
import { panelThresholdDisplay, thresholdBackground, thresholdRegions } from "@/lib/workspace-threshold-display"
import { workspaceAnnotationMarkers } from "@/lib/workspace-annotations"
import { applyFillBelowRows, fillBelowBands, fillBelowOpacity, fillBelowTooltipValue, type FillBelowBand } from "@/lib/workspace-fill-below"
import { WorkspaceGradientDefs } from "./gradient-fill"
import { WorkspaceValueAxis } from "./value-axis"
import { WorkspaceLegend } from "./legend"
import { formatWorkspaceValue, workspaceAxisTime } from "@/lib/workspace-format"
import { interpolationCurve, panelLineStyle, pointRadius, stacksSeries } from "@/lib/workspace-line-style"
import { percentStackGroups, stackedTooltipValueByRef, type StackTotals } from "@/lib/workspace-stack"
import { axisScale, maskLogRows } from "@/lib/workspace-axis-scale"
import { axisGrid, axisPlacement } from "@/lib/workspace-axis-display"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult, MetricResultSeries, WorkspaceRange } from "@/types/metric-workspace"
import { dateTimeFormatter } from "@/lib/intl-format"

export function WorkspaceChart(props: {
  panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number
  onRange: (range: WorkspaceRange) => void
  /** v25 面板注释：看板级注释叠加到每个面板，面板级注释只叠加到所属面板。 */
  annotations?: DashboardAnnotation[]
}) {
  const { locale, t } = useLocale()
  const style = panelLineStyle(props.panel)
  const gradient = panelGradientMode(props.panel.gradient_mode)
  const curve = interpolationCurve(style.interpolation)
  const [hidden, setHidden] = useState<string[]>([])
  const [drag, setDrag] = useState<[number, number] | null>(null)
  const logarithmic = axisScale(props.panel) === "log"
  const merged = useMemo(() => mergeWorkspaceSeries(props.result), [props.result])
  // 对数刻度下 0 与负数没有位置：与 Grafana 一样把它们当作缺失样本，而不是画在轴外。
  const rows = useMemo(() => logarithmic ? maskLogRows(merged, props.result.series.map((value) => value.ref_id)) : merged,
    [merged, logarithmic, props.result.series])
  const stackMode = style.stack
  const percent = stackMode === "percent"
  const stacked = stacksSeries(stackMode)
  // 百分比堆叠需要归一化坐标与还原表；还原表让 tooltip 继续显示原始值（Grafana 的做法）。
  // 双 Y 轴：每条序列按覆盖或面板声明落在左轴或右轴；只有两侧都分到序列时才画第二根轴。
  const sides = useMemo(() => axisSideGroup(props.result.series, (value) => workspaceSeriesAxisPlacement(props.panel, value)),
    [props.result.series, props.panel])
  // Overrides → Axis 的逐序列轴选项：命中的覆盖按顺序叠加，后面的赢；没有命中就沿用面板级设置。
  const leftAxisOptions = useMemo(() => workspaceSideAxisOptions(props.panel, sides.left), [props.panel, sides.left])
  const rightAxisOptions = useMemo(() => workspaceSideAxisOptions(props.panel, sides.right), [props.panel, sides.right])
  const dual = axisHasBothSides(sides)
  // 面板声明右轴、但序列全被覆盖挪到左轴时只剩一根轴，轴标签跟着唯一的那根轴走。
  const single: AxisSide = !dual && sides.left.length === 0 && sides.right.length > 0 ? "right" : "left"
  const stackedData = useMemo<{ rows: Record<string, number | null>[]; totals: Map<string, StackTotals> | null }>(() => percent
    ? percentStackGroups(rows, [sides.left.map((value) => value.ref_id), sides.right.map((value) => value.ref_id)])
    : { rows, totals: null }, [rows, percent, sides])
  const chartRows = stackedData.rows, stackTotals = stackedData.totals
  const series = props.result.series.filter((value) => !hidden.includes(value.ref_id))
  // 填充带只画在可见序列上：派生键写进同一份行数据，因此缺口与堆叠语义和折线完全一致。
  const bands = useMemo(() => fillBelowBands(props.panel, props.result.series).filter((band) => !hidden.includes(band.refId)),
    [props.panel, props.result.series, hidden])
  const bandRows = useMemo(() => applyFillBelowRows(chartRows, bands), [chartRows, bands])
  // 颜色与图例名先算一次：渐变定义与序列渲染必须用同一份颜色，否则渐变会脱离折线色。
  const painted = series.map((value, index) => ({ ref_id: value.ref_id, value, side: workspaceSeriesAxisPlacement(props.panel, value),
    color: workspaceSeriesColor(props.panel, value, props.expanded, index),
    name: workspaceSeriesName(props.panel, value, props.expanded), lineWidth: workspaceSeriesLineWidth(props.panel, value, style.lineWidth),
    decimals: workspaceSeriesDecimals(props.panel, value),
    fillOpacity: workspaceSeriesFillOpacity(props.panel, value, style.fillOpacity) }))
  // tooltip 的分组名与小数位都按序列解析：命中覆盖的序列在自己的行上用自己的位数。
  const decimalsByRef = new Map(painted.map((entry) => [entry.ref_id, entry.decimals]))
  const unit = props.result.series[0]?.unit ?? "number"
  // 百分比堆叠时坐标轴按百分比刻度显示，tooltip 仍用原始单位与原始值；否则每根轴用它自己第一条序列的单位。
  const axisUnit = (group: MetricResultSeries[]) => percent ? "percent" : group[0] ? workspaceSeriesUnit(props.panel, group[0]) : unit
  const from = Date.parse(props.result.from), to = Date.parse(props.result.to)
  // 阈值显示模式：off 关掉阈值线，line 画虚线，area 画色带，background 铺绘图区背景。
  const thresholdDisplay = panelThresholdDisplay(props.panel)
  const regions = thresholdRegions(props.panel)
  const background = thresholdBackground(props.panel, series.map(latestWorkspaceValue))
  const markers = useMemo(() => workspaceAnnotationMarkers(props.annotations ?? [], props.panel.id, from, to),
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 标记只由注释列表、面板与查询窗口决定
    [props.annotations, props.panel.id, from, to])
  function zoom() {
    if (drag && Math.abs(drag[1] - drag[0]) >= 1000) props.onRange({ mode: "absolute", from: new Date(Math.min(...drag)).toISOString(), to: new Date(Math.max(...drag)).toISOString() })
    setDrag(null)
  }
  // 图例放到右侧时外层改成 flex：ResponsiveContainer 会被压窄、图例保持固定宽度；
  // sr-only 的说明节点是绝对定位，不参与 flex 布局。
  const legendRight = props.panel.legend && legendPlacement(props.panel) === "right"
  return <div className={`min-w-0 ${legendRight ? "flex items-stretch" : ""}`} aria-label={props.panel.title}>
    <ResponsiveContainer width="100%" height={props.height ?? 224}>
      {/* ComposedChart 而不是 LineChart：recharts 3 只在 AreaChart/ComposedChart 里渲染 Area，
          用 LineChart 时「填充不透明度」会整条序列消失。 */}
      <ComposedChart data={bandRows} margin={{ top: 12, right: 24, bottom: 8, left: 0 }}
        onMouseDown={(event) => { const value = chartTimestamp(event.activeLabel); if (value !== null) setDrag([value, value]) }}
        onMouseMove={(event) => { const value = chartTimestamp(event.activeLabel); if (drag && value !== null) setDrag([drag[0], value]) }}
        onMouseUp={zoom} onMouseLeave={() => setDrag(null)}>
        {axisGrid(props.panel) && <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />}
        <WorkspaceGradientDefs panelId={props.panel.id} mode={gradient} opacity={style.fillOpacity / 100} direction="down"
          series={painted.map(({ ref_id, color, fillOpacity }) => ({ ref_id, color, opacity: fillOpacity / 100 }))} />
        <XAxis dataKey="timestamp" type="number" domain={[from, to]} allowDataOverflow tickLine={false} axisLine={false} minTickGap={40} fontSize={11}
          tickFormatter={(value) => workspaceAxisTime(Number(value), to - from, locale)} />
        {(dual || single === "left") && <WorkspaceValueAxis side="left" unit={axisUnit(sides.left)} maximum={axisSideMaximum(sides.left)}
          labelled={!dual || axisPlacement(props.panel) === "left"} panel={props.panel} locale={locale} options={leftAxisOptions} />}
        {(dual || single === "right") && <WorkspaceValueAxis side="right" unit={axisUnit(sides.right)} maximum={axisSideMaximum(sides.right)}
          labelled={!dual || axisPlacement(props.panel) === "right"} panel={props.panel} locale={locale} options={rightAxisOptions} />}
        {props.panel.tooltip !== "off" && <Tooltip shared={props.panel.tooltip !== "single"} labelFormatter={(label) => `${dateTimeFormatter(locale, { timeZone: "UTC", hour12: false }).format(Number(label))} UTC`} formatter={(value, name, item) => [formatWorkspaceValue(fillBelowTooltipValue(stackedTooltipValueByRef(Number(value), item, stackTotals), item, bands), unit, { locale, decimals: decimalsByRef.get(String(tooltipRefId(item))) ?? props.panel.decimals, noValue: workspaceNoValue(props.panel) }), name]}
          contentStyle={{ background: "var(--popover)", borderColor: "var(--border)", borderRadius: 8, fontSize: 12 }} />}
        {markers.map((marker) => <ReferenceArea key={marker.annotation.id} x1={marker.start} x2={marker.end} y1="dataMin" y2="dataMax"
          fill={panelColors[marker.annotation.color]} fillOpacity={0.14} stroke="none" />)}
        {markers.map((marker) => <ReferenceLine key={`${marker.annotation.id}-line`} x={marker.start}
          stroke={panelColors[marker.annotation.color]} strokeDasharray="2 2" />)}
        {background && <ReferenceArea x1={from} x2={to} y1="dataMin" y2="dataMax" fill={panelColors[background]} fillOpacity={0.1} stroke="none" />}
        {regions.map((region) => <ReferenceArea key={`${region.from}-${String(region.to)}-${region.color}`} x1={from} x2={to} y1={region.from} y2={region.to}
          fill={panelColors[region.color]} fillOpacity={0.14} stroke="none" />)}
        {thresholdDisplay === "line" && props.panel.thresholds.map((threshold) => <ReferenceLine key={threshold.value} y={threshold.value} stroke={panelColors[threshold.color]} strokeDasharray="4 4" />)}
        {painted.map(({ ref_id, side, color, name, lineWidth, fillOpacity }) => {
          const band = bandFor(bands, ref_id, side)
          // 数据点：never 不画点，其余按 point_size，未设置时沿用 auto 1.8px / always 2.6px 的既有口径。
          const dot = style.showPoints === "never" ? false : { r: pointRadius(style), strokeWidth: 0, fill: color }
          // 渐变模式下不透明度写进停靠点，整层填充保持不透明；纯色模式仍按 fillOpacity 叠加。
          const fill = gradientFill(props.panel.id, ref_id, gradient, color)
          // 线型与线宽、插值一样只作用于描边，因此面积层与折线层共用同一个虚线数组。
          const dash = linePatternDash(style.pattern)
          const connect = style.connectNulls === "always"
          if (band) return <Fragment key={ref_id}>
            {/* 基线层不可见：它只把堆叠的起点抬到终点序列上，增量层的上沿因此落在自己的折线上。 */}
            <Area dataKey={band.baseKey} yAxisId={side} type={curve} fill="transparent" stroke="none" stackId={band.stackId}
              tooltipType="none" dot={false} connectNulls={connect} isAnimationActive={false} />
            <Area name={name} dataKey={band.deltaKey} yAxisId={side} type={curve} stroke={color} strokeWidth={lineWidth} strokeDasharray={dash}
              fill={fill} fillOpacity={gradient === "none" ? fillBelowOpacity(fillOpacity) / 100 : 1} stackId={band.stackId} dot={dot} connectNulls={connect} isAnimationActive={false} />
          </Fragment>
          return fillOpacity > 0 || stacked
            ? <Area key={ref_id} name={name} dataKey={ref_id} yAxisId={side} type={curve} stroke={color} strokeWidth={lineWidth} fill={fill} strokeDasharray={dash}
                fillOpacity={gradient === "none" ? fillOpacity / 100 : 1} stackId={stacked ? `beat-stack-${side}` : undefined} dot={dot} connectNulls={connect} isAnimationActive={false} />
            : <Line key={ref_id} name={name} dataKey={ref_id} yAxisId={side} type={curve} stroke={color} strokeWidth={lineWidth} strokeDasharray={dash} dot={dot} connectNulls={connect} isAnimationActive={false} />
        })}
        {drag && <ReferenceArea x1={Math.min(...drag)} x2={Math.max(...drag)} fill="var(--foreground)" fillOpacity={0.08} />}
      </ComposedChart>
    </ResponsiveContainer>
    {props.panel.legend && <WorkspaceLegend panel={props.panel} result={props.result} expanded={props.expanded} hidden={hidden}
      toggle={(refId) => setHidden((current) => current.includes(refId) ? current.filter((id) => id !== refId) : [...current, refId])} />}
    <span className="sr-only">{t("workspace.drag_zoom")}</span>
    {markers.length > 0 && <ul className="sr-only" aria-label={`${props.panel.title} · annotations`}>
      {markers.map((marker) => <li key={marker.annotation.id}>{`${marker.annotation.title} · ${marker.annotation.from} → ${marker.annotation.to}`}</li>)}
    </ul>}
  </div>
}

/** tooltipRefId 从 recharts 的 tooltip 条目里取序列 ref_id：逐序列小数位按它查表。 */
function tooltipRefId(item: unknown): string {
  return String((item as { dataKey?: unknown } | undefined)?.dataKey ?? "")
}

/** bandFor 找到该序列的填充带：终点落在另一根轴上时不画，避免跨轴比较两个刻度。 */
function bandFor(bands: FillBelowBand[], refId: string, side: AxisSide): FillBelowBand | undefined {
  return bands.find((band) => band.refId === refId && band.side === side)
}