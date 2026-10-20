import { useLocale } from "@/context/locale"
import { formatWorkspaceValue, workspaceAxisTime } from "@/lib/workspace-format"
import { heatmapCellColor, heatmapGrid, heatmapIntensity, panelHeatmap } from "@/lib/workspace-heatmap"
import type { DashboardPanel, MetricQueryResult, PanelColor } from "@/types/metric-workspace"

/** 取值轴刻度数量：与 Grafana 的默认刻度密度一致，过多标签会互相压住。 */
const axisTickCount = 5

/** Grafana 的 Heatmap：时间做列、取值分桶做行，单元格亮度表示落在该格的样本数量。 */
export function WorkspaceHeatmap(props: { panel: DashboardPanel; result: MetricQueryResult; height?: number }) {
  const { locale, t } = useLocale()
  const style = panelHeatmap(props.panel)
  const grid = heatmapGrid(props.panel, props.result)
  const unit = props.result.series[0]?.unit ?? ""
  const format = (value: number) => formatWorkspaceValue(value, unit, { locale, decimals: props.panel.decimals })
  if (!grid.columns.length || !grid.rows.length) return <div className="grid h-60 place-content-center text-sm text-muted-foreground">{t("workspace.no_data")}</div>
  const duration = grid.columns[grid.columns.length - 1].to - grid.columns[0].from
  const timeLabel = (timestamp: number) => `${workspaceAxisTime(timestamp, duration, locale)} UTC`
  // 取值轴自下而上画，界面自上而下排：最热的桶显示在最上面。
  const displayRows = grid.counts.map((_, index) => grid.rows.length - 1 - index)
  return <div role="img" className="flex flex-col gap-2 px-4 py-3" style={{ minHeight: props.height ?? 224 }} aria-label={props.panel.title}>
    <div className="flex min-h-32 flex-1 gap-2">
      <div className="flex w-16 shrink-0 flex-col justify-between py-px text-right text-[10px] text-muted-foreground tabular-nums">
        {valueAxisLabels(grid.rows[grid.rows.length - 1].to, grid.rows[0].from).map((value, index) => <span key={index}>{format(value)}</span>)}
      </div>
      <div className="grid min-w-0 flex-1 gap-px" style={{ gridTemplateColumns: `repeat(${grid.columns.length}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${grid.rows.length}, minmax(0, 1fr))` }}>
        {displayRows.map((row) => grid.columns.map((column, index) => {
          const count = grid.counts[row][index]
          return <div key={`${row}-${index}`} data-heatmap-cell
            title={`${timeLabel(column.from)} · ${format(grid.rows[row].from)} – ${format(grid.rows[row].to)} · ${formatWorkspaceValue(count, "", { locale })}`}
            style={{ backgroundColor: heatmapCellColor(style.color, heatmapIntensity(count, grid.max, style.scale)) }} />
        }))}
      </div>
    </div>
    <div className="flex justify-between pl-[4.5rem] text-[10px] text-muted-foreground tabular-nums">
      <span>{timeLabel(grid.columns[0].from)}</span>
      <span>{timeLabel(grid.columns[Math.floor(grid.columns.length / 2)].from)}</span>
      <span>{timeLabel(grid.columns[grid.columns.length - 1].to)}</span>
    </div>
    {props.panel.legend && <HeatmapScale color={style.color} max={grid.max} total={grid.total} label={t("workspace.heatmap_samples")} />}
  </div>
}

/** 取值轴标签：把可见区间等分，顶端是最热的取值上界。 */
function valueAxisLabels(highest: number, lowest: number): number[] {
  return Array.from({ length: axisTickCount }, (_, index) => highest - ((highest - lowest) * index) / (axisTickCount - 1))
}

/** 色标只说明颜色与样本数的关系，不参与交互；数值与总数同时给出，避免只能靠颜色比较。 */
function HeatmapScale(props: { color: PanelColor; max: number; total: number; label: string }) {
  const stops = [0, 0.25, 0.5, 0.75, 1].map((ratio) => heatmapCellColor(props.color, ratio))
  return <div className="flex items-center gap-2 pl-[4.5rem] text-[10px] text-muted-foreground">
    <span className="tabular-nums">0</span>
    <span aria-hidden="true" className="h-2 w-24 rounded-sm" style={{ backgroundImage: `linear-gradient(90deg, ${stops.join(", ")})` }} />
    <span className="tabular-nums">{props.max}</span>
    <span>{props.label} · {props.total}</span>
  </div>
}
