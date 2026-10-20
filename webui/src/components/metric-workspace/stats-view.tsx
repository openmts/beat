import { ArrowDownIcon, ArrowUpIcon, DownloadIcon } from "lucide-react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { downloadWorkspaceFile } from "@/lib/workspace-export"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { sortWorkspaceStats, workspaceSeriesStats, workspaceStatsCSV, workspaceStatsTotals, type WorkspaceStatsColumn } from "@/lib/workspace-stats"
import { workspaceNoValue } from "@/lib/workspace-overrides"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult } from "@/types/metric-workspace"

/** Grafana 检查器的 Stats 视图：每条序列一行，表头可排序，可导出 CSV；缺数据的统计格显示占位符。 */
export function WorkspaceStatsTable(props: { result: MetricQueryResult; expanded?: ExpandedSeries[]; title: string; panel?: DashboardPanel }) {
  const { locale, t } = useLocale()
  const [sort, setSort] = useState<{ column: WorkspaceStatsColumn; direction: "asc" | "desc" } | null>(null)
  const columns: WorkspaceStatsColumn[] = ["label", "min", "max", "mean", "first", "last", "samples", "gaps"]
  const labels = [t("workspace.legend.series"), t("workspace.legend.min"), t("workspace.legend.max"), t("workspace.legend.mean"),
    t("workspace.legend.first"), t("workspace.legend.last"), t("workspace.legend.count"), t("workspace.legend.gaps")]
  // 面板用于解析字段覆盖后的显示单位；检查器与表格面板必须给出同一份单位口径。
  const rows = workspaceSeriesStats(props.result, props.expanded, props.panel)
  const sorted = sort ? sortWorkspaceStats(rows, sort.column, sort.direction) : rows
  const totals = workspaceStatsTotals(rows)
  const cell = (value: number | null, unit: string) => value === null ? workspaceNoValue(props.panel) : formatWorkspaceValue(value, unit, { locale, decimals: props.panel?.decimals })
  const toggle = (column: WorkspaceStatsColumn) => setSort((current) => current?.column === column
    ? { column, direction: current.direction === "asc" ? "desc" : "asc" }
    : { column, direction: "asc" })
  return <div className="px-4 pb-3">
    <div className="flex justify-end py-2">
      <Button variant="outline" size="sm" onClick={() => downloadWorkspaceFile(props.title, workspaceStatsCSV(sorted, labels), "csv")}>
        <DownloadIcon />{t("workspace.export_stats_csv")}</Button>
    </div>
    <div className="max-h-72 overflow-auto">
      <table className="w-full text-left text-xs">
        <thead className="sticky top-0 bg-card">
          <tr className="border-b">
            {columns.map((column, index) => <th key={column} aria-sort={sort?.column === column ? sort.direction === "asc" ? "ascending" : "descending" : "none"}
              className={`py-2 pr-4 font-medium ${index === 0 ? "" : "text-right"} ${index === columns.length - 1 ? "pr-0" : ""}`}>
              <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => toggle(column)}>
                {labels[index]}{sort?.column === column && (sort.direction === "asc" ? <ArrowUpIcon className="size-3" aria-hidden /> : <ArrowDownIcon className="size-3" aria-hidden />)}
              </button></th>)}
          </tr>
        </thead>
        <tbody>
          {sorted.map((stats) => (
            <tr key={stats.key} className="border-b last:border-0">
              <td className="max-w-48 truncate py-2 pr-4" title={stats.label}>{stats.label}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{cell(stats.min, stats.unit)}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{cell(stats.max, stats.unit)}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{cell(stats.mean, stats.unit)}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{cell(stats.first, stats.unit)}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{cell(stats.last, stats.unit)}</td>
              <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{stats.samples}</td>
              <td className="whitespace-nowrap py-2 text-right font-mono" title={stats.gaps > 0 ? t("workspace.legend_gaps_hint") : undefined}
                data-gaps={stats.gaps}>{stats.gaps > 0 ? <span className="font-medium text-destructive">{stats.gaps}</span> : stats.gaps}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="border-t text-muted-foreground">
            <td className="py-2 pr-4" colSpan={columns.length - 2}>{t("workspace.stats_totals", { series: String(totals.series), coverage: coverageText(totals.coverage) })}</td>
            <td className="whitespace-nowrap py-2 pr-4 text-right font-mono">{totals.samples}</td>
            <td className="whitespace-nowrap py-2 text-right font-mono">{totals.gaps}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  </div>
}

/** 覆盖率按整数百分比显示；没有任何分桶时给占位符，而不是谎报 0%。 */
function coverageText(coverage: number | null): string {
  return coverage === null ? "—" : `${Math.round(coverage * 100)}%`
}
