import type { ExpandedSeries, MetricQuery, MetricQueryResult } from "@/types/metric-workspace"

/** 导出用的 CSV 单元格转义：公式前缀加单引号，内部引号翻倍。 */
export function workspaceCSVCell(value: string): string {
  const safe = /^[\s]*[=+@-]/.test(value) ? `'${value}` : value
  return `"${safe.replaceAll('"', '""')}"`
}

export function workspaceCSV(result: MetricQueryResult, expanded: ExpandedSeries[] = []): string {
  const devices = result.series.some(series => Boolean(series.device_id))
  const rows = [devices ? "timestamp,series,node,device,metric,aggregation,unit,value" : "timestamp,series,node,metric,aggregation,unit,value"]
  for (const series of result.series) {
    const presentation = expanded.find((candidate) => candidate.ref_id === series.ref_id)
    for (const point of series.points) {
      rows.push([new Date(point.timestamp).toISOString(), presentation?.label || series.label,
        presentation?.node_name || series.node_id, ...(devices ? [series.device_id ?? ""] : []), series.metric, series.aggregation, series.unit]
        .map(workspaceCSVCell).concat(point.value == null ? "" : String(point.value)).join(","))
    }
  }
  return rows.join("\r\n")
}

export function workspaceJSON(query: MetricQuery, result: MetricQueryResult): string {
  return JSON.stringify({ query, result }, null, 2)
}

export function downloadWorkspaceFile(name: string, content: string, type: "csv" | "json"): void {
  const blob = new Blob([type === "csv" ? "\uFEFF" : "", content], { type: type === "csv" ? "text/csv;charset=utf-8" : "application/json" })
  const location = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = location
  link.download = `${name.replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 80) || "metrics"}.${type}`
  try { document.body.append(link); link.click() } finally { link.remove(); setTimeout(() => URL.revokeObjectURL(location), 0) }
}
