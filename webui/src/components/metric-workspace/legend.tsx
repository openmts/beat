import { useLocale } from "@/context/locale"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { seriesLegendStatistics, workspaceLegendOrder, type LegendStatistics } from "@/lib/workspace-chart"
import { legendPlacement, legendWidth, type LegendPlacement } from "@/lib/workspace-legend-placement"
import { useLegendText } from "./legend-text"
import { legendHidden, workspaceSeriesDecimals, workspaceSeriesName, workspaceSeriesUnit } from "@/lib/workspace-overrides"
import type { DashboardPanel, ExpandedSeries, LegendValue, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

type LegendProps = {
  panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]
  hidden: string[]; toggle: (refId: string) => void
}

export function WorkspaceLegend(props: LegendProps) {
  const { locale } = useLocale()
  const text = useLegendText()
  const values = props.panel.legend_values ?? []
  // 覆盖里的 hide_legend 只影响图例：序列仍然绘制，只是不在这里列出。
  const entries = workspaceLegendOrder(props.panel, props.result).filter((series) => !legendHidden(props.panel, series))
  const placement = legendPlacement(props.panel)
  // 显式声明列宽时用行内宽度覆盖自带类名，避免两个宽度来源互相打架。
  const width = legendWidth(props.panel)
  const widthStyle = width === undefined ? undefined : { width }
  if (!values.length) return <div className={placementClass(placement, width !== undefined)} style={widthStyle}>
    {entries.map((series) => <LegendName key={series.ref_id} panel={props.panel} series={series} expanded={props.expanded} hidden={props.hidden} toggle={props.toggle} />)}
  </div>
  return <div className={placement === "right" ? `${width === undefined ? "w-56 " : ""}shrink-0 overflow-x-auto px-2 pb-1` : "overflow-x-auto px-3 pb-1"} style={widthStyle}>
    <table className="w-full text-left text-xs">
      <thead><tr className="border-b text-muted-foreground">
        <th className="py-1.5 pr-4 font-medium">{text("series")}</th>
        {values.map((value) => <th key={value} className="py-1.5 pl-4 text-right font-medium">{text(value)}</th>)}
      </tr></thead>
      <tbody>{entries.map((series) => {
        const statistics = seriesLegendStatistics(series)
        return <tr key={series.ref_id} className="border-b last:border-0">
          <td className="max-w-64 py-1.5 pr-4"><LegendName panel={props.panel} series={series} expanded={props.expanded} hidden={props.hidden} toggle={props.toggle} /></td>
          {values.map((value) => <td key={value} className="py-1.5 pl-4 text-right font-mono tabular-nums">{legendCell(value, { statistics, series, unit: workspaceSeriesUnit(props.panel, series) }, { locale, decimals: workspaceSeriesDecimals(props.panel, series) })}</td>)}
        </tr>
      })}</tbody>
    </table>
  </div>
}

/** placementClass 决定图例容器的排布：右侧是固定宽度的竖列，底部沿用居中的多行流式排布。 */
function placementClass(placement: LegendPlacement, sized: boolean): string {
  return placement === "right"
    ? `flex ${sized ? "" : "w-40 "}shrink-0 flex-col gap-1 overflow-y-auto px-2`
    : "flex flex-wrap justify-center gap-x-4 gap-y-1 px-3"
}

function LegendName(props: { panel: DashboardPanel; series: MetricResultSeries; expanded?: ExpandedSeries[]; hidden: string[]; toggle: (refId: string) => void }) {
  // 截断的系列名必须保留完整 tooltip，长标签不能因为 max-w 限制而无法辨认。
  const label = workspaceSeriesName(props.panel, props.series, props.expanded)
  const muted = props.hidden.includes(props.series.ref_id)
  return <button type="button" title={label} aria-pressed={!muted}
    className={`max-w-64 truncate text-xs ${muted ? "text-muted-foreground line-through" : "text-foreground"}`}
    onClick={() => props.toggle(props.series.ref_id)}>{label}</button>
}

function legendCell(value: LegendValue, entry: { statistics: LegendStatistics; series: MetricResultSeries; unit: string }, options: { locale: string; decimals?: number }): string {
  return formatWorkspaceValue(entry.statistics[value], entry.unit, options)
}
