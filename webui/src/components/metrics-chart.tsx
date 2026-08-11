import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts"
import type { MetricDataPoint } from "@/types"
import { useLocale } from "@/context/locale"
import { formatMetricValue, type MetricValueFormat } from "@/lib/metric-format"

interface MetricsChartProps {
  data?: MetricDataPoint[]
  title: string
  color: string
  valueFormat: MetricValueFormat
  from: number
  to: number
  rangeHours: number
}

function toDate(value: number | string): Date | null {
  const ts = typeof value === "number" ? value : Number(value)
  return Number.isNaN(ts) ? null : new Date(ts * 1000)
}

function formatAxisTimestamp(value: number | string, rangeHours: number): string {
  const date = toDate(value)
  if (!date) return ""
  if (rangeHours >= 168) {
    return date.toLocaleString(undefined, {
      month: "numeric",
      day: "numeric",
      hour: "2-digit",
    })
  }
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
}

function formatTooltipTimestamp(value: number | string): string {
  const date = toDate(value)
  if (!date) return ""
  return date.toLocaleString(undefined, {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
}

export function MetricsChart({
  data,
  title,
  color,
  valueFormat,
  from,
  to,
  rangeHours,
}: MetricsChartProps) {
  const { t } = useLocale()
  if (!data || data.length === 0) {
    return (
      <div className="flex h-56 flex-col gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <div className="flex flex-1 items-center justify-center border border-dashed text-sm text-muted-foreground">
          {t("metrics.empty")}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-sm font-medium">{title}</h3>
      <ResponsiveContainer width="100%" height={192}>
        <LineChart data={data} margin={{ top: 4, right: 4, bottom: 4, left: 4 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="timestamp"
            type="number"
            scale="linear"
            domain={[from, to]}
            allowDataOverflow
            tickFormatter={(value) => formatAxisTimestamp(value, rangeHours)}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
          />
          <YAxis
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
            domain={valueFormat === "percent" ? [0, 100] : ["auto", "auto"]}
            tickFormatter={(value) => formatMetricValue(value, valueFormat)}
          />
          <Tooltip
            labelFormatter={(label) => formatTooltipTimestamp(label as number)}
            formatter={(value) => [formatMetricValue(Number(value), valueFormat), title]}
            contentStyle={{
              fontSize: "12px",
              borderRadius: "8px",
              border: "1px solid var(--border)",
            }}
          />
          <Line
            type="monotone"
            dataKey="value"
            stroke={color}
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
