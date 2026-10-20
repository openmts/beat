import { useLocale } from "@/context/locale"
import { formatPercent } from "@/lib/intl-format"
import { panelColors } from "@/lib/workspace-chart"
import { workspaceNoValue, workspaceSeriesColor, workspaceSeriesDecimals, workspaceSeriesName, workspaceSeriesUnit } from "@/lib/workspace-overrides"
import { panelCalculation, panelCalculationValue } from "@/lib/workspace-calculation"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import { panelPieChart, pieArcPath, pieSliceFractions, pieSliceStarts, type PanelPieChart } from "@/lib/workspace-pie-chart"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

/** 扇区外半径与环形内半径比例；固定尺寸让同一看板在任意面板宽度下形状一致。 */
const pieRadius = 76
const donutInnerRatio = 0.55

interface PieEntry {
  refId: string
  label: string
  text: string
  color: string
  value: number | null
  fraction: number
}

/** Grafana 的 Pie chart：每条序列一个扇区，标签按百分比/数值显示，可切换实心饼图与环形图。 */
export function WorkspacePieChart(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number }) {
  const { locale, t } = useLocale()
  const style = panelPieChart(props.panel)
  const entries = props.result.series.map((series) => pieEntry(props.panel, series, props.expanded, locale))
  const fractions = pieSliceFractions(entries.map((entry) => entry.value))
  const starts = pieSliceStarts(fractions)
  const innerRadius = style.chartType === "donut" ? pieRadius * donutInnerRatio : 0
  const diameter = pieRadius + innerRadius
  return <div className="flex flex-col items-center justify-center gap-6 px-6 py-4 sm:flex-row sm:justify-start" style={{ minHeight: props.height ?? 224 }}>
    <PieGraphic entries={entries} fractions={fractions} starts={starts} innerRadius={innerRadius} diameter={diameter} label={props.panel.title} />
    <ul aria-label={t("workspace.pie_legend", { title: props.panel.title })} className="w-full min-w-0 space-y-2">
      {entries.map((entry, index) => <li key={entry.refId} className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 text-xs">
        <span aria-hidden="true" className="size-2.5 rounded-full" style={{ backgroundColor: entry.color }} />
        <span className="truncate text-muted-foreground" title={entry.label}>{entry.label}</span>
        <span className="whitespace-nowrap font-mono tabular-nums">{pieLabelText(entry, style, fractions[index], locale)}</span>
      </li>)}
    </ul>
  </div>
}

function PieGraphic(props: { entries: PieEntry[]; fractions: number[]; starts: number[]; innerRadius: number; diameter: number; label: string }) {
  const size = props.diameter * 2
  return <svg role="img" aria-label={props.label} viewBox={`0 0 ${size} ${size}`} width={size / 2} height={size / 2} className="shrink-0">
    {props.fractions.map((fraction, index) => <Slice key={props.entries[index].refId} entry={props.entries[index]} fraction={fraction} start={props.starts[index]}
      innerRadius={props.innerRadius} diameter={props.diameter} />)}
  </svg>
}

/** 单个扇区；占比为 100% 时无法用两段等角度弧表示，改用整圆（环形图用描边圆环）。 */
function Slice(props: { entry: PieEntry; fraction: number; start: number; innerRadius: number; diameter: number }) {
  const { entry, fraction, start, innerRadius, diameter } = props
  const full = fraction >= 1
  if (full && innerRadius > 0) {
    return <circle data-slice={entry.refId} cx={diameter} cy={diameter} r={(diameter + innerRadius) / 2}
      fill="none" stroke={entry.color} strokeWidth={diameter - innerRadius} />
  }
  if (full) return <circle data-slice={entry.refId} cx={diameter} cy={diameter} r={diameter} fill={entry.color} />
  if (fraction <= 0) return null
  return <path data-slice={entry.refId} d={pieArcPath({ radius: diameter, innerRadius }, start, start + fraction)} fill={entry.color} />
}

/** 标签内容：percent/value/both，百分比由 Intl 决定符号位置。 */
function pieLabelText(entry: PieEntry, style: PanelPieChart, fraction: number, locale: string): string {
  const percent = formatPercent(locale, fraction * 100)
  if (style.labelMode === "value") return entry.text
  if (style.labelMode === "both") return `${entry.text} · ${percent}`
  return percent
}

/** 扇区取色：命中值映射时用映射颜色，否则按重引用取该序列的作者配色（与 Grafana 每扇区一色一致）。 */
function pieEntry(panel: DashboardPanel, series: MetricResultSeries, expanded: ExpandedSeries[] | undefined, locale: string): PieEntry {
  const value = panelCalculationValue(series, panelCalculation(panel))
  const mapped = mappedWorkspaceValue(panel, value)
  return {
    refId: series.ref_id,
    label: workspaceSeriesName(panel, series, expanded),
    text: mapped?.text ?? formatWorkspaceValue(value, workspaceSeriesUnit(panel, series),
      { locale, decimals: workspaceSeriesDecimals(panel, series), noValue: workspaceNoValue(panel) }),
    color: mapped?.color ? panelColors[mapped.color] : workspaceSeriesColor(panel, series, expanded),
    value,
    fraction: 0,
  }
}
