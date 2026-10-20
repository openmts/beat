import { useLocale } from "@/context/locale"
import { barGaugeFraction, barGaugeLitSegments, barGaugeSegments, panelBarGauge, resolvedBarOrientation } from "@/lib/workspace-bar-gauge"
import { panelColors } from "@/lib/workspace-chart"
import {
  overrideColor, workspaceSeriesDecimals, workspaceSeriesLimits, workspaceSeriesName, workspaceSeriesThresholdColor,
  workspaceSeriesUnit,
} from "@/lib/workspace-overrides"
import { panelCalculation, panelCalculationValue } from "@/lib/workspace-calculation"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import type { BarDisplay, DashboardPanel, ExpandedSeries, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

interface BarGaugeEntry {
  refId: string
  label: string
  text: string
  color: string
  minimum: number
  maximum: number
  fraction: number
  value: number | null
}

/** Grafana 的 Bar gauge：每条序列一根条，阈值取色，可切换方向、填充方式与未填充轨道。 */
export function WorkspaceBarGauge(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number }) {
  const { locale } = useLocale()
  const style = panelBarGauge(props.panel)
  const entries = props.result.series.map((series) => barGaugeEntry(props.panel, series, props.expanded, locale))
  const vertical = resolvedBarOrientation(props.panel, entries.length) === "vertical"
  const track = { display: style.display, unfilled: style.unfilled }
  return <div className="flex flex-col justify-center gap-4 px-6" style={{ minHeight: props.height ?? 224 }} aria-label={props.panel.title}>
    {vertical
      ? <div className="flex items-end justify-around gap-4">{entries.map((entry) =>
        <div key={entry.refId} className="flex min-w-0 flex-1 flex-col items-center gap-2">
          <BarValue entry={entry} />
          <BarTrack entry={entry} {...track} vertical />
          <BarLabel entry={entry} />
        </div>)}</div>
      : <div className="flex flex-col gap-3">{entries.map((entry) =>
        <div key={entry.refId} className="grid grid-cols-[minmax(5rem,9rem)_1fr_auto] items-center gap-3">
          <BarLabel entry={entry} />
          <BarTrack entry={entry} {...track} />
          <BarValue entry={entry} />
        </div>)}</div>}
  </div>
}

function BarLabel(props: { entry: BarGaugeEntry }) {
  return <span className="max-w-full truncate text-xs text-muted-foreground" title={props.entry.label}>{props.entry.label}</span>
}

function BarValue(props: { entry: BarGaugeEntry }) {
  return <span className="whitespace-nowrap font-mono text-sm tabular-nums" style={{ color: props.entry.color }}>{props.entry.text}</span>
}

function meterProps(entry: BarGaugeEntry) {
  return {
    role: "meter" as const, "aria-label": entry.label, "aria-valuenow": entry.value ?? entry.minimum,
    "aria-valuemin": entry.minimum, "aria-valuemax": entry.maximum, "aria-valuetext": entry.text,
  }
}

function BarTrack(props: { entry: BarGaugeEntry; display: BarDisplay; unfilled: boolean; vertical?: boolean }) {
  const { entry, vertical } = props
  const track = props.unfilled ? "bg-muted" : "border border-border bg-transparent"
  const size = vertical ? "relative h-40 w-10" : "h-4 w-full"
  if (props.display === "lcd") {
    const lit = barGaugeLitSegments(entry.fraction)
    return <div {...meterProps(entry)} className={`${size} flex gap-0.5 rounded-sm p-0.5 ${track} ${vertical ? "flex-col-reverse" : ""}`}>
      {Array.from({ length: barGaugeSegments }, (_, cell) => <span key={cell} className="flex-1 rounded-[1px]" style={{ backgroundColor: cell < lit ? entry.color : "transparent" }} />)}
    </div>
  }
  const fill = props.display === "gradient" ? `linear-gradient(${vertical ? "0deg" : "90deg"}, transparent, ${entry.color})` : entry.color
  const filled = vertical ? { height: `${entry.fraction * 100}%` } : { width: `${entry.fraction * 100}%` }
  return <div {...meterProps(entry)} className={`${size} overflow-hidden rounded-sm ${track}`}>
    <div className={vertical ? "absolute bottom-0 left-0 w-full" : "h-full"} style={{ backgroundColor: fill, ...filled }} />
  </div>
}

/** 每条序列的取值、标尺与取色；值映射优先于阈值取色，未命中时仍按数值与阈值渲染。 */
function barGaugeEntry(panel: DashboardPanel, series: MetricResultSeries, expanded: ExpandedSeries[] | undefined, locale: string): BarGaugeEntry {
  const value = panelCalculationValue(series, panelCalculation(panel))
  const mapped = mappedWorkspaceValue(panel, value)
  const unit = workspaceSeriesUnit(panel, series)
  // 逐序列覆盖优先：Grafana 的 Overrides → Standard options → Min / Max 只对有条状量程的面板生效。
  const limits = workspaceSeriesLimits(panel, series)
  const minimum = limits.min ?? 0
  const maximum = limits.max ?? (unit === "percent" ? 100 : Math.max(minimum + 1, value ?? 0, ...series.points.map((point) => point.value ?? 0)))
  return {
    refId: series.ref_id,
    label: workspaceSeriesName(panel, series, expanded),
    text: mapped?.text ?? formatWorkspaceValue(value, unit, { locale, decimals: workspaceSeriesDecimals(panel, series) }),
    color: mapped?.color ? panelColors[mapped.color] : overrideColor(panel, series) ?? workspaceSeriesThresholdColor(panel, series, value),
    minimum, maximum, fraction: barGaugeFraction(minimum, maximum, value), value,
  }
}
