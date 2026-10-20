import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { histogramBarRatio, histogramDistributions, histogramPeak, panelHistogram } from "@/lib/workspace-histogram"
import type { DashboardPanel, MetricQueryResult, PanelColor } from "@/types/metric-workspace"

/** 计数轴刻度数量：与 Grafana 的默认刻度密度一致，过多标签会互相压住。 */
const axisTickCount = 5

/** Grafana 的 Histogram：取值做横轴分桶、桶内样本数做柱高，可合并多条序列。 */
export function WorkspaceHistogram(props: { panel: DashboardPanel; result: MetricQueryResult; height?: number }) {
  const { locale, t } = useLocale()
  const distributions = histogramDistributions(props.panel, props.result)
  const total = distributions.reduce((sum, distribution) => sum + distribution.total, 0)
  if (!distributions.length || !total) return <div className="grid h-60 place-content-center text-sm text-muted-foreground">{t("workspace.no_data")}</div>
  const bins = distributions[0].bins
  const peak = histogramPeak(distributions)
  const unit = distributions[0].unit
  const scale = panelHistogram(props.panel).scale
  const format = (value: number) => formatWorkspaceValue(value, unit, { locale, decimals: props.panel.decimals })
  const countLabel = (value: number) => formatWorkspaceValue(value, "", { locale })
  return <div role="img" className="flex flex-col gap-2 px-4 py-3" style={{ minHeight: props.height ?? 224 }} aria-label={props.panel.title}>
    <div className="flex min-h-32 flex-1 gap-2">
      <div className="flex w-16 shrink-0 flex-col justify-between py-px text-right text-[10px] text-muted-foreground tabular-nums">
        {countAxisLabels(peak).map((value, index) => <span key={index}>{countLabel(value)}</span>)}
      </div>
      <div className="flex min-w-0 flex-1 items-end gap-px" data-histogram-buckets={bins.length}>
        {bins.map((bin, index) => <div key={index} className="flex min-w-0 flex-1 items-end justify-center gap-px" data-histogram-bucket={index}
          title={`${format(bin.from)} – ${format(bin.to)} · ${countLabel(bin.count)}`}>
          {distributions.map((distribution) => <div key={distribution.refId} data-histogram-bar
            className="w-full rounded-t-sm" style={{ height: `${Math.max(1, histogramBarRatio(distribution.bins[index].count, peak, scale) * 100)}%`, backgroundColor: barColor(distribution.color, distribution.total, total) }} />)}
        </div>)}
      </div>
    </div>
    <div className="flex justify-between pl-[4.5rem] text-[10px] text-muted-foreground tabular-nums">
      <span>{format(bins[0].from)}</span>
      <span>{format(bins[Math.floor(bins.length / 2)].from)}</span>
      <span>{format(bins[bins.length - 1].to)}</span>
    </div>
    {props.panel.legend && <HistogramLegend distributions={distributions} total={total} label={t("workspace.histogram_samples")} />}
  </div>
}

/** 计数轴标签：把 0..峰值等分，顶端是峰值。 */
function countAxisLabels(peak: number): number[] {
  return Array.from({ length: axisTickCount }, (_, index) => Math.round((peak * (axisTickCount - 1 - index)) / (axisTickCount - 1)))
}

/** 合并后的分布只用一个配色；多序列沿用各序列的颜色，保持与折线、柱状图一致的图例语义。 */
function barColor(color: PanelColor, seriesTotal: number, total: number): string {
  const hex = panelColors[color]
  const alpha = seriesTotal === total ? 1 : 0.85
  const red = Number.parseInt(hex.slice(1, 3), 16), green = Number.parseInt(hex.slice(3, 5), 16), blue = Number.parseInt(hex.slice(5, 7), 16)
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`
}

/** 图例同时给出样本总数，避免只靠柱子高低比较分布。 */
function HistogramLegend(props: { distributions: { refId: string; label: string; color: PanelColor; total: number }[]; total: number; label: string }) {
  return <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pl-[4.5rem] text-[10px] text-muted-foreground">
    {props.distributions.map((distribution) => <span key={distribution.refId} className="flex items-center gap-1">
      <span aria-hidden="true" className="size-2 rounded-sm" style={{ backgroundColor: panelColors[distribution.color] }} />
      <span>{distribution.label}</span>
      <span className="tabular-nums">{distribution.total}</span>
    </span>)}
    <span>{props.label} · {props.total}</span>
  </div>
}
