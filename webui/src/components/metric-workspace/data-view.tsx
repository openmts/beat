import { ArrowDownIcon, ArrowUpIcon, DownloadIcon } from "lucide-react"
import { lazy, Suspense, useState } from "react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { latestWorkspacePoint, panelColors } from "@/lib/workspace-chart"
import {
  overrideColor, workspaceNoValue, workspaceSeriesDecimals, workspaceSeriesLimits, workspaceSeriesName,
  workspaceSeriesThresholdColor, workspaceSeriesUnit,
} from "@/lib/workspace-overrides"
import { WorkspaceBarGauge } from "./bar-gauge"
import { WorkspacePieChart } from "./pie-chart"
import { WorkspaceChart } from "./chart"
import { WorkspaceBarChart } from "./bar-chart"
import { WorkspaceHeatmap } from "./heatmap"
import { WorkspaceHistogram } from "./histogram"
import { WorkspaceStateTimeline } from "./state-timeline"
import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import { applyPanelTransformationsWithFrame, type WorkspaceTableFrame } from "@/lib/workspace-transformations"
import { panelCalculation, panelCalculationValue } from "@/lib/workspace-calculation"
import { panelSparkline } from "@/lib/workspace-sparkline"
import { panelStatStyle, statAppearanceStyles, statOrientationLayout, statTextSizeClass } from "@/lib/workspace-stat"
import { gaugeThresholdMarks, panelGaugeThresholds } from "@/lib/workspace-gauge"
import { maximumTableRows, sortWorkspaceTableRows, tableCellStyle, workspaceTableCSV, workspaceTableColumns, workspaceTableRows, type WorkspaceTableColumn } from "@/lib/workspace-table"
import { panelTableOrientation } from "@/lib/workspace-table"
import { WorkspaceWideTable } from "./wide-table"
import { WorkspaceFrameTable } from "./frame-table"
import { downloadWorkspaceFile } from "@/lib/workspace-export"
import { WorkspaceSparkline } from "./sparkline"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult, WorkspaceRange } from "@/types/metric-workspace"
import { formatDateTime } from "@/lib/intl-format"

const LazyGeoMap = lazy(async () => ({ default: (await import("./geomap")).WorkspaceGeoMap }))

export function WorkspaceDataView(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; onRange: (range: WorkspaceRange) => void; height?: number; annotations?: DashboardAnnotation[] }) {
  const { t } = useLocale()
  // 面板变换是渲染前的一步：过滤掉全部序列时同样要走「无数据」空状态。
  const transformed = applyPanelTransformationsWithFrame(props.panel, props.result)
  const result = transformed.result
  const empty = result.series.length === 0 || result.series.every((series) => series.points.every((point) => point.value === null))
  if (empty) return <div className="grid h-60 place-content-center gap-2 px-6 text-center"><p className="text-sm font-medium">{t("workspace.no_data")}</p><p className="text-xs text-muted-foreground">{t("workspace.no_data_description")}</p></div>
  if (props.panel.type === "stat" || props.panel.type === "gauge") return <WorkspaceStat {...props} result={result} />
  if (props.panel.type === "table") return <WorkspaceDataTable {...props} result={result} frame={transformed.frame} />
  if (props.panel.type === "bargauge") return <WorkspaceBarGauge panel={props.panel} result={result} expanded={props.expanded} height={props.height} />
  if (props.panel.type === "piechart") return <WorkspacePieChart panel={props.panel} result={result} expanded={props.expanded} height={props.height} />
  if (props.panel.type === "heatmap") return <WorkspaceHeatmap panel={props.panel} result={result} height={props.height} />
  if (props.panel.type === "histogram") return <WorkspaceHistogram panel={props.panel} result={result} height={props.height} />
  // geo map 带着世界底图数据，按需加载：公共节点页的首屏预算不因为它变大。
  if (props.panel.type === "geomap") return <Suspense fallback={<Skeleton className="m-4 h-52" />}>
    <LazyGeoMap panel={props.panel} result={result} expanded={props.expanded} height={props.height} />
  </Suspense>
  if (props.panel.type === "statetimeline") return <WorkspaceStateTimeline panel={props.panel} result={result} expanded={props.expanded} height={props.height} />
  if (props.panel.type === "barchart") return <WorkspaceBarChart panel={props.panel} result={result} expanded={props.expanded} height={props.height} onRange={props.onRange} />
  return <WorkspaceChart {...props} result={result} />
}

function WorkspaceStat(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[] }) {
  const { locale, t } = useLocale()
  const series = props.result.series[0]
  // Grafana 的 Value options → Calculation：折线保留采样点，取值面板先算成一个数再渲染。
  const value = panelCalculationValue(series, panelCalculation(props.panel))
  // 值映射优先于阈值取色：命中时用映射文本与颜色，未命中仍按数值与阈值渲染。
  const mapped = mappedWorkspaceValue(props.panel, value)
  const text = mapped?.text ?? formatWorkspaceValue(value, workspaceSeriesUnit(props.panel, series),
    { locale, decimals: workspaceSeriesDecimals(props.panel, series), noValue: workspaceNoValue(props.panel) })
  // 取色优先级：值映射 > 字段覆盖 > 阈值 > 序列自带色。
  const color = mapped?.color ? panelColors[mapped.color] : overrideColor(props.panel, series) ?? workspaceSeriesThresholdColor(props.panel, series, value)
  // 逐序列 Min/Max（Grafana 的 Overrides → Standard options）优先，其次才是面板级上下限。
  const limits = workspaceSeriesLimits(props.panel, series)
  const minimum = limits.min ?? 0
  const maximum = limits.max ?? (series.unit === "percent" ? 100 : Math.max(1, value ?? 0, ...series.points.map((point) => point.value ?? 0)))
  const progress = value === null ? 0 : Math.max(0, Math.min(100, (value - minimum) / (maximum - minimum) * 100))
  // Stat styles：Color mode 决定阈值色落在数值还是面板底色上，Text mode 决定主文本与附属说明显示什么。
  const style = panelStatStyle(props.panel)
  const appearance = statAppearanceStyles(props.panel, color)
  const caption = style.colorMode === "background" ? "text-current opacity-80" : "text-muted-foreground"
  const label = workspaceSeriesName(props.panel, series, props.expanded)
  // name 把系列名顶到主文本，none 整块文本都不渲染；两者都不再输出附属的系列名与采样时间。
  const body = style.textMode === "name" ? label : text
  // Gauge 的阈值刻度与标签复用同一份阈值：刻度按位置画在条上，标签只在刻度开启时渲染。
  const gaugeStyle = panelGaugeThresholds(props.panel)
  const gaugeMarks = props.panel.type === "gauge" ? gaugeThresholdMarks(props.panel, minimum, maximum) : []
  // Orientation 决定名称与数值怎么排：inline 同行，stacked-above 名称在上，stacked-below 沿用旧顺序。
  const layout = statOrientationLayout(style.orientation)
  const valueText = style.textMode !== "none" && <div className={`font-mono ${statTextSizeClass(style.textSize)} font-medium tabular-nums tracking-tight`} style={appearance.value}>{body}</div>
  const seriesName = style.textMode === "auto" && <div className={`truncate text-xs ${caption}`} title={label}>{label}</div>
  return <div className="flex h-60 flex-col justify-center gap-5 px-8" style={appearance.container}>
    {layout === "inline" ? <div className="flex w-full items-baseline justify-center gap-3">{valueText}{seriesName}</div>
      : <>{layout === "stacked-above" && seriesName}{valueText}{layout === "stacked-below" && seriesName}</>}
    {props.panel.type === "stat" && panelSparkline(props.panel) && <WorkspaceSparkline panel={props.panel} series={series} color={appearance.accent} />}
    {style.textMode === "auto" && <div className={`text-xs ${caption}`}>{t("workspace.sample_time", { time: latestWorkspacePoint(series) ? formatDateTime(locale, latestWorkspacePoint(series)!.timestamp) : workspaceNoValue(props.panel) })}</div>}
    {props.panel.type === "gauge" && <div>
      <div className="relative h-3 overflow-hidden rounded-full bg-muted" role="meter" aria-label={props.panel.title} aria-valuenow={value === null ? minimum : Math.max(minimum, Math.min(maximum, value))} aria-valuemin={minimum} aria-valuemax={maximum} aria-valuetext={text}>
        <div className="h-full rounded-full" style={{ width: `${Number.isFinite(progress) ? progress : 0}%`, backgroundColor: color }} />
        {/* Grafana Gauge → Show threshold markers：按阈值位置在仪表条上画一列刻度。 */}
        {gaugeStyle.markers && gaugeMarks.map((mark) => <span key={mark.value} aria-hidden className="absolute inset-y-0 w-0.5 -translate-x-1/2" style={{ left: `${mark.ratio * 100}%`, backgroundColor: panelColors[mark.color] }} />)}
      </div>
      {gaugeStyle.labels && gaugeMarks.length > 0 && <div className="relative h-4" aria-hidden>
        {gaugeMarks.map((mark) => <span key={mark.value} className="absolute -translate-x-1/2 whitespace-nowrap font-mono text-[10px] text-muted-foreground" style={{ left: `${mark.ratio * 100}%` }}>{formatWorkspaceValue(mark.value, series.unit, { locale })}</span>)}
      </div>}
      <div className="mt-2 flex justify-between font-mono text-xs text-muted-foreground"><span>{formatWorkspaceValue(minimum, series.unit, { locale })}</span><span>{formatWorkspaceValue(maximum, series.unit, { locale })}</span></div>
    </div>}
  </div>
}

/** 面板里的表格套用值映射；检查器（不传 panel）保持原始数值，便于核对数据。宽表形态交给宽表渲染。 */
export function WorkspaceDataTable(props: { result: MetricQueryResult; expanded?: ExpandedSeries[]; panel?: DashboardPanel; frame?: WorkspaceTableFrame | null }) {
  // 结果帧优先：链尾是 Reduce → Series to rows 或 Group by 时，表格要按 Grafana 的帧渲染。
  if (props.frame && props.panel) return <WorkspaceFrameTable frame={props.frame} panel={props.panel} />
  if (props.panel && panelTableOrientation(props.panel) === "wide") return <WorkspaceWideTable result={props.result} expanded={props.expanded} panel={props.panel} />
  return <WorkspaceLongTable {...props} />
}

/** WorkspaceLongTable 是历史的表格形态：一行一个采样，列固定为时间/序列/数值。 */
function WorkspaceLongTable(props: { result: MetricQueryResult; expanded?: ExpandedSeries[]; panel?: DashboardPanel }) {
  const { locale, t } = useLocale()
  const [sort, setSort] = useState<{ column: WorkspaceTableColumn; direction: "asc" | "desc" } | null>(null)
  const labels = { timestamp: t("workspace.timestamp"), label: t("workspace.metric"), value: t("workspace.value") }
  // 表格面板丢掉空桶（查询按 max_points 返回定长网格）；检查器与 Explore 保留原始结果以便核对。
  const rows = workspaceTableRows(props.result, props.expanded, { dropEmptyBuckets: props.panel !== undefined, panel: props.panel })
  const sorted = sort ? sortWorkspaceTableRows(rows, sort.column, sort.direction) : rows
  const shown = sorted.slice(0, maximumTableRows)
  const toggle = (column: WorkspaceTableColumn) => setSort((current) => current?.column === column
    ? { column, direction: current.direction === "asc" ? "desc" : "asc" }
    : { column, direction: "asc" })
  return <div className="px-4 pb-3">
    <div className="flex justify-end py-2">
      <Button variant="outline" size="sm" onClick={() => downloadWorkspaceFile(props.panel?.title ?? t("workspace.data"), workspaceTableCSV(shown, workspaceTableColumns.map((column) => labels[column])), "csv")}>
        <DownloadIcon />{t("workspace.export_table_csv")}</Button>
    </div>
    <div className="max-h-72 overflow-auto">
      <table className="w-full text-left text-xs"><thead className="sticky top-0 bg-card"><tr className="border-b">
        {workspaceTableColumns.map((column, index) => <th key={column} aria-sort={sort?.column === column ? sort.direction === "asc" ? "ascending" : "descending" : "none"}
          className={`py-2 font-medium ${index === workspaceTableColumns.length - 1 ? "text-right" : "pr-4"}`}>
          <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => toggle(column)}>
            {labels[column]}{sort?.column === column && (sort.direction === "asc" ? <ArrowUpIcon className="size-3" aria-hidden /> : <ArrowDownIcon className="size-3" aria-hidden />)}
          </button></th>)}
      </tr></thead>
      <tbody>{shown.map((row) => {
        const mapped = props.panel ? mappedWorkspaceValue(props.panel, row.value) : null
        const style = props.panel ? tableCellStyle(props.panel, { value: row.value, series: { ref_id: row.refID, label: row.label } }, mapped?.color ? panelColors[mapped.color] : undefined) : undefined
        return <tr key={row.key} className="border-b last:border-0">
        <td className="whitespace-nowrap py-2 pr-4 font-mono">{new Date(row.timestamp).toISOString().replace("T", " ").replace(".000Z", "")}</td>
        <td className="max-w-48 truncate py-2 pr-4" title={row.label}>{row.label}</td><td className="whitespace-nowrap py-2 text-right font-mono" style={style}>{mapped?.text ?? formatWorkspaceValue(row.value, row.unit, { locale })}</td>
      </tr>
      })}</tbody>
    </table>
    {rows.length > maximumTableRows && <p className="pt-3 text-xs text-muted-foreground">{t("workspace.inspector_limit", { rows: String(rows.length) })}</p>}
    </div>
  </div>
}
