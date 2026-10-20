import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"

import { useLocale } from "@/context/locale"
import { formatDiagnosticValue } from "@/lib/diagnostics-format"
import { formatTime } from "@/lib/intl-format"
import type { DiagnosticSeries } from "@/lib/diagnostics-api"

export interface DiagnosticChartLine {
  label: string
  color: string
  series: DiagnosticSeries | null
}

interface DiagnosticsChartProps {
  title: string
  metric: string
  lines: DiagnosticChartLine[]
}

// 图表只展示已确认入库的数值；没有数据时给出明确空态而不是空白坐标系。
export function DiagnosticsChart(props: DiagnosticsChartProps) {
  const { locale, t } = useLocale()
  const rows = mergeSeries(props.lines, locale)
  if (rows.length === 0) {
    return (
      <section className="flex flex-col gap-2">
        <h4 className="text-sm font-medium">{props.title}</h4>
        <p className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm text-muted-foreground">{t("diagnostics.no_series")}</p>
      </section>
    )
  }
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-3">
        <h4 className="text-sm font-medium">{props.title}</h4>
        <ul className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          {props.lines.map((line) => (
            <li key={line.label} className="flex items-center gap-1">
              <span aria-hidden className="inline-block size-2 rounded-full" style={{ backgroundColor: line.color }} />
              {line.label}
            </li>
          ))}
        </ul>
      </div>
      <div className="h-56 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={rows} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
            <XAxis dataKey="label" tick={{ fontSize: 11 }} />
            <YAxis width={64} tick={{ fontSize: 11 }} tickFormatter={(value) => formatDiagnosticValue(props.metric, Number(value), locale)} />
            <Tooltip formatter={(value) => formatDiagnosticValue(props.metric, Number(value), locale)} />
            {props.lines.map((line) => (
              <Line key={line.label} type="monotone" dataKey={line.label} stroke={line.color} dot={false} strokeWidth={2} connectNulls />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </section>
  )
}

function mergeSeries(lines: DiagnosticChartLine[], locale?: string): Record<string, number | string>[] {
  const rows = new Map<number, Record<string, number | string>>()
  for (const line of lines) {
    for (const point of line.series?.points ?? []) {
      const timestamp = new Date(point.Timestamp).getTime()
      if (Number.isNaN(timestamp)) continue
      const row = rows.get(timestamp) ?? { timestamp, label: formatTime(locale, timestamp) }
      row[line.label] = point.Value
      rows.set(timestamp, row)
    }
  }
  return [...rows.values()].sort((left, right) => Number(left.timestamp) - Number(right.timestamp))
}
