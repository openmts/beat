import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { workspaceNoValue, workspaceSeriesName } from "@/lib/workspace-overrides"
import { formatWorkspaceValue, workspaceAxisTime } from "@/lib/workspace-format"
import { panelStateTimeline, stateTimeline, timelineShowsLabel, type TimelineState } from "@/lib/workspace-state-timeline"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult } from "@/types/metric-workspace"

/** Grafana 的状态时间线：每条序列一行，行内按时间顺序排列状态色块。 */
export function WorkspaceStateTimeline(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number }) {
  const { locale, t } = useLocale()
  const style = panelStateTimeline(props.panel)
  const unit = props.result.series[0]?.unit ?? ""
  // 面板级 No value 在这里替换默认破折号；状态时间线按行取序列名，逐序列覆盖同样生效。
  const format = (value: number | null) => formatWorkspaceValue(value, unit,
    { locale, decimals: props.panel.decimals, noValue: workspaceNoValue(props.panel) })
  const label = (state: TimelineState) => state.text ?? format(state.value)
  const timeline = stateTimeline(props.panel, props.result, { label: (series) => workspaceSeriesName(props.panel, series, props.expanded) || series.ref_id })
  if (!timeline.rows.length) return <div className="grid h-60 place-content-center text-sm text-muted-foreground">{t("workspace.no_data")}</div>
  const span = Math.max(1, timeline.to - timeline.from)
  const timeLabel = (timestamp: number) => `${workspaceAxisTime(timestamp, span, locale)} UTC`
  const width = (segment: { from: number; to: number }) => `${Math.max(0, (segment.to - segment.from) / span) * 100}%`
  return <div role="img" className="flex flex-col gap-3 px-4 py-3" style={{ minHeight: props.height ?? 224 }} aria-label={props.panel.title}>
    <div className="flex flex-col gap-1.5">
      {timeline.rows.map((row) => <div key={row.refId} className="flex items-center gap-2">
        <span className="w-28 shrink-0 truncate text-xs text-muted-foreground" title={row.label}>{row.label}</span>
        <div className="flex h-6 min-w-0 flex-1 overflow-hidden rounded-sm bg-muted" data-timeline-row={row.refId}>
          {row.segments.map((segment) => <div key={`${segment.from}-${segment.state?.key ?? "none"}`} data-timeline-segment
            className="flex items-center overflow-hidden px-1 text-[10px] text-white/95"
            title={`${timeLabel(segment.from)} – ${timeLabel(segment.to)} · ${segment.state ? label(segment.state) : t("workspace.no_data")}`}
            style={{ width: width(segment), backgroundColor: segment.state ? panelColors[segment.state.color] : "transparent" }}>
            {segment.state && timelineShowsLabel(style.showValues, (segment.to - segment.from) / span) && <span className="truncate">{label(segment.state)}</span>}
          </div>)}
        </div>
      </div>)}
    </div>
    <div className="flex justify-between pl-[7.5rem] text-[10px] text-muted-foreground tabular-nums">
      <span>{timeLabel(timeline.from)}</span>
      <span>{timeLabel(timeline.from + span / 2)}</span>
      <span>{timeLabel(timeline.to)}</span>
    </div>
    {props.panel.legend && <StateLegend label={t("workspace.state_timeline_states")} states={timeline.states} text={label} />}
  </div>
}

/** 图例只列出真正渲染出来的状态，并给出各自的块数；没有图例时状态仍然写在各块的提示里。 */
function StateLegend(props: { label: string; states: { state: TimelineState; count: number }[]; text: (state: TimelineState) => string }) {
  return <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 pl-[7.5rem] text-[10px] text-muted-foreground" aria-label={props.label}>
    {props.states.map((entry) => <li key={entry.state.key} className="flex items-center gap-1.5">
      <span aria-hidden="true" className="size-2 rounded-sm" style={{ backgroundColor: panelColors[entry.state.color] }} />
      <span className="truncate">{props.text(entry.state)}</span>
      <span className="tabular-nums">{entry.count}</span>
    </li>)}
  </ul>
}
