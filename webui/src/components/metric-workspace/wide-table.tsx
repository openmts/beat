import { ArrowDownIcon, ArrowUpIcon, DownloadIcon } from "lucide-react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { downloadWorkspaceFile } from "@/lib/workspace-export"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { maximumTableRows, sortWorkspaceWideRows, tableCellStyle, workspaceWideCSV, workspaceWideColumns, workspaceWideRows, type WorkspaceWideColumn, type WorkspaceWideRow } from "@/lib/workspace-table"
import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import { workspaceNoValue, workspaceSeriesDecimals } from "@/lib/workspace-overrides"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult } from "@/types/metric-workspace"

/** 宽表形态（Grafana 的 Table 默认布局）：一列一条序列、一行一个时间点；缺样本的单元格留空。 */
export function WorkspaceWideTable(props: { result: MetricQueryResult; expanded?: ExpandedSeries[]; panel: DashboardPanel }) {
  const { locale, t } = useLocale()
  const [sort, setSort] = useState<WorkspaceWideSort | null>(null)
  const columns = workspaceWideColumns(props.result, props.expanded, { panel: props.panel })
  const rows = workspaceWideRows(props.result, columns)
  const sorted = sort ? sortWorkspaceWideRows(rows, sort.column, sort.direction) : rows
  const shown = sorted.slice(0, maximumTableRows)
  const headers = [t("workspace.timestamp"), ...columns.map((column) => column.label)]
  return <div className="px-4 pb-3">
    <div className="flex justify-end py-2">
      <Button variant="outline" size="sm" onClick={() => downloadWorkspaceFile(props.panel.title, workspaceWideCSV(shown, headers), "csv")}>
        <DownloadIcon />{t("workspace.export_table_csv")}</Button>
    </div>
    <div className="max-h-72 overflow-auto">
      <table className="w-full text-left text-xs"><thead className="sticky top-0 bg-card"><tr className="border-b">
        <SortHeader label={t("workspace.timestamp")} active={sort?.column === null} direction={sort?.direction} onToggle={() => toggle(sort, setSort, null)} />
        {columns.map((column, index) => <SortHeader key={column.key} label={column.label} active={sort?.column === index} direction={sort?.direction} onToggle={() => toggle(sort, setSort, index)} />)}
      </tr></thead>
        <tbody>{shown.map((row) => <WideRow key={row.key} row={row} columns={columns} panel={props.panel} locale={locale} />)}</tbody>
      </table>
      {rows.length > maximumTableRows && <p className="pt-3 text-xs text-muted-foreground">{t("workspace.inspector_limit", { rows: String(rows.length) })}</p>}
    </div>
  </div>
}

interface WorkspaceWideSort {
  column: number | null
  direction: "asc" | "desc"
}

/** toggle 按 Grafana 的两档排序：第一次升序，再点同一个表头切降序。 */
function toggle(current: WorkspaceWideSort | null, update: (next: WorkspaceWideSort) => void, column: number | null): void {
  update(current?.column === column ? { column, direction: current.direction === "asc" ? "desc" : "asc" } : { column, direction: "asc" })
}

function SortHeader(props: { label: string; active: boolean; direction?: "asc" | "desc"; onToggle: () => void }) {
  return <th aria-sort={props.active ? props.direction === "desc" ? "descending" : "ascending" : "none"} className="whitespace-nowrap px-2 py-2 font-medium">
    <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={props.onToggle}>
      {props.label}{props.active && (props.direction === "desc" ? <ArrowDownIcon className="size-3" aria-hidden /> : <ArrowUpIcon className="size-3" aria-hidden />)}
    </button></th>
}

/** WideRow 每格都走与长表同一套取值路径：值映射优先，其后才是逐序列阈值取色与单位。 */
function WideRow(props: { row: WorkspaceWideRow; columns: WorkspaceWideColumn[]; panel: DashboardPanel; locale: string }) {
  return <tr className="border-b last:border-0">
    <td className="whitespace-nowrap px-2 py-2 font-mono">{new Date(props.row.timestamp).toISOString().replace("T", " ").replace(".000Z", "")}</td>
    {props.columns.map((column, index) => <WideCell key={column.key} value={props.row.cells[index] ?? null} column={column} panel={props.panel} locale={props.locale} />)}
  </tr>
}

function WideCell(props: { value: number | null; column: WorkspaceWideColumn; panel: DashboardPanel; locale: string }) {
  const mapped = mappedWorkspaceValue(props.panel, props.value)
  const style = tableCellStyle(props.panel, { value: props.value, series: { ref_id: props.column.refID, label: props.column.label } }, mapped?.color ? panelColors[mapped.color] : undefined)
  const missing = props.value === null || !Number.isFinite(props.value)
  return <td className="whitespace-nowrap px-2 py-2 text-right font-mono" style={style}>
    {missing ? workspaceNoValue(props.panel) : mapped?.text ?? formatWorkspaceValue(props.value, props.column.unit, { locale: props.locale, decimals: workspaceSeriesDecimals(props.panel, { ref_id: props.column.refID, label: props.column.label, unit: props.column.unit }) })}</td>
}
