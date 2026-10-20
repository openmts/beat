import { ArrowDownIcon, ArrowUpIcon, DownloadIcon } from "lucide-react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { downloadWorkspaceFile } from "@/lib/workspace-export"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { maximumTableRows, sortWorkspaceFrameRows, tableCellStyle, workspaceFrameCSV } from "@/lib/workspace-table"
import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import { workspaceNoValue, workspaceSeriesDecimals, workspaceSeriesUnit } from "@/lib/workspace-overrides"
import type { WorkspaceFrameCell, WorkspaceFrameColumn, WorkspaceTableFrame } from "@/lib/workspace-transformations"
import type { DashboardPanel } from "@/types/metric-workspace"

/**
 * 结果帧表格：Grafana 的 Reduce → Series to rows（一行一个字段、一列一个计算方式）与 Group by
 * （一行一个分组、一列一个 `<分组字段> (计算方式)`）在表格面板里的形态。键列直接给文本，数值列
 * 走与其它表格同一套取值路径：值映射优先，其后是逐序列阈值取色与单位格式化。
 */
export function WorkspaceFrameTable(props: { frame: WorkspaceTableFrame; panel: DashboardPanel }) {
  const { locale, t } = useLocale()
  const [sort, setSort] = useState<{ column: number; direction: "asc" | "desc" } | null>(null)
  const headers = props.frame.columns.map((column) => frameColumnLabel(column, t))
  const rows = sort ? sortWorkspaceFrameRows(props.frame.rows, props.frame.columns, sort.column, sort.direction) : props.frame.rows
  const shown = rows.slice(0, maximumTableRows)
  const toggle = (column: number) => setSort((current) => current?.column === column
    ? { column, direction: current.direction === "asc" ? "desc" : "asc" }
    : { column, direction: "asc" })
  return <div className="px-4 pb-3">
    <div className="flex justify-end py-2">
      <Button variant="outline" size="sm" onClick={() => downloadWorkspaceFile(props.panel.title, workspaceFrameCSV(shown, headers), "csv")}>
        <DownloadIcon />{t("workspace.export_table_csv")}</Button>
    </div>
    <div className="max-h-72 overflow-auto">
      <table className="w-full text-left text-xs"><thead className="sticky top-0 bg-card"><tr className="border-b">
        {props.frame.columns.map((column, index) => <th key={column.key} aria-sort={sort?.column === index ? sort.direction === "asc" ? "ascending" : "descending" : "none"}
          className={`whitespace-nowrap px-2 py-2 font-medium ${column.value ? "text-right" : "pr-4"}`}>
          <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => toggle(index)}>
            {headers[index]}{sort?.column === index && (sort.direction === "asc" ? <ArrowUpIcon className="size-3" aria-hidden /> : <ArrowDownIcon className="size-3" aria-hidden />)}
          </button></th>)}
      </tr></thead>
        <tbody>{shown.map((row) => <tr key={row.key} className="border-b last:border-0">
          {props.frame.columns.map((column, index) => <FrameCell key={column.key} column={column} cell={row.cells[index]} panel={props.panel} locale={locale} />)}
        </tr>)}</tbody>
      </table>
      {rows.length > maximumTableRows && <p className="pt-3 text-xs text-muted-foreground">{t("workspace.inspector_limit", { rows: String(rows.length) })}</p>}
    </div>
  </div>
}

/** frameColumnLabel 拼出 Grafana 的列名：可本地化的字段名，数值列再带上 `(计算方式)` 后缀。 */
function frameColumnLabel(column: WorkspaceFrameColumn, t: (key: string) => string): string {
  const base = column.label ?? (column.labelKey ? t(column.labelKey) : "")
  return column.calculationKey ? `${base} (${t(column.calculationKey)})` : base
}

/** FrameCell 只在数值列上取色与格式化；键列永远按文本渲染，缺失的数值格显示破折号。 */
function FrameCell(props: { column: WorkspaceFrameColumn; cell?: WorkspaceFrameCell; panel: DashboardPanel; locale: string }) {
  if (!props.column.value) return <td className="whitespace-nowrap px-2 py-2 pr-4">{props.cell?.text ?? ""}</td>
  const value = props.cell?.value ?? null
  const series = props.cell?.series
  const mapped = mappedWorkspaceValue(props.panel, value)
  const style = tableCellStyle(props.panel, { value, series }, mapped?.color ? panelColors[mapped.color] : undefined)
  // 数值列一般带着来源序列（逐序列覆盖与阈值都按它解析）；没有来源时退回面板级单位。
  const unit = props.cell?.unit ?? (series ? workspaceSeriesUnit(props.panel, series) : props.panel.unit || "auto")
  return <td className="whitespace-nowrap px-2 py-2 text-right font-mono" style={style}>
    {value === null ? workspaceNoValue(props.panel) : mapped?.text ?? formatWorkspaceValue(value, unit, { locale: props.locale, decimals: workspaceSeriesDecimals(props.panel, series) })}</td>
}
