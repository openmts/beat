import { useEffect, useMemo, useState } from "react"
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"

import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useLocale } from "@/context/locale"
import { useNetworkHistory } from "@/hooks/use-network"
import { dateTimeFormatter, formatDateTime } from "@/lib/intl-format"
import {
  carrierColor,
  formatLatency,
  formatPercent,
  networkHistoryRanges,
  networkNodeLabel,
} from "@/lib/network-quality"
import type { NetworkCarrier, NetworkHistorySeries, NetworkTaskView } from "@/types"

interface NetworkHistoryDialogProps {
  open: boolean
  view: NetworkTaskView | null
  initialNodeId?: string
  admin?: boolean
  onOpenChange: (open: boolean) => void
}

type ChartMetric = "latency" | "quality"

export function NetworkHistoryDialog({
  open,
  view,
  initialNodeId,
  admin = false,
  onOpenChange,
}: NetworkHistoryDialogProps) {
  const { t } = useLocale()
  const [nodeId, setNodeId] = useState(initialNodeId ?? "")
  const [rangeHours, setRangeHours] = useState(24)
  const [metric, setMetric] = useState<ChartMetric>("latency")

  useEffect(() => {
    if (!open) return
    setNodeId(initialNodeId ?? view?.nodes[0]?.node.id ?? "")
    setRangeHours(24)
    setMetric("latency")
  }, [initialNodeId, open, view])

  const history = useNetworkHistory({
    taskId: view?.task.id,
    nodeId,
    rangeHours,
    admin,
    enabled: open,
  })
  const nodeOptions = useMemo(() => view?.nodes.map(({ node }) => ({
    label: networkNodeLabel(node),
    value: node.id,
  })) ?? [], [view])
  const series = history.data?.series ?? []
  const qualityKind = view?.task.type === "icmp" ? "loss" : "success"
  const chart = useMemo(() => buildChartData(series, metric, qualityKind), [metric, qualityKind, series])
  const qualityLabel = view?.task.type === "icmp" ? t("network.packet_loss") : t("network.success_rate")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{view?.task.name ?? t("network.history")}</DialogTitle>
          <DialogDescription>{describeTask(view)}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
            <Select items={nodeOptions} value={nodeId} onValueChange={(value) => setNodeId(value ?? "")}>
              <SelectTrigger aria-label={t("network.node")} className="w-full lg:w-64">
                <SelectValue placeholder={t("network.select_node")} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {nodeOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <div className="flex flex-wrap items-center gap-2">
              <ToggleGroup
                variant="outline"
                size="sm"
                spacing={0}
                value={[metric]}
                onValueChange={(value) => value[0] && setMetric(value[0] as ChartMetric)}
              >
                <ToggleGroupItem value="latency" aria-label={t("network.latency")}>{t("network.latency")}</ToggleGroupItem>
                <ToggleGroupItem value="quality" aria-label={qualityLabel}>{qualityLabel}</ToggleGroupItem>
              </ToggleGroup>
              <ToggleGroup
                variant="outline"
                size="sm"
                spacing={0}
                value={[String(rangeHours)]}
                onValueChange={(value) => {
                  const next = Number(value[0])
                  if (next > 0) setRangeHours(next)
                }}
              >
                {networkHistoryRanges.map((range) => (
                  <ToggleGroupItem key={range.hours} value={String(range.hours)} aria-label={range.label}>
                    {range.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          </div>
          {history.error ? (
            <Alert variant="destructive"><AlertDescription>{history.error}</AlertDescription></Alert>
          ) : history.loading ? (
            <Skeleton className="h-80 w-full" />
          ) : chart.data.length === 0 ? (
            <div className="flex h-80 items-center justify-center border border-dashed text-sm text-muted-foreground">
              {t("network.no_history")}
            </div>
          ) : (
            <NetworkHistoryChart
              data={chart.data}
              series={chart.series}
              metric={metric}
              from={history.from}
              to={history.to}
              rangeHours={rangeHours}
              latencyLabel={t("network.latency")}
              qualityLabel={qualityLabel}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface ChartSeries {
  key: string
  label: string
  carrier: NetworkCarrier
  color: string
}

interface HistoryChartProps {
  data: Array<Record<string, number | null>>
  series: ChartSeries[]
  metric: ChartMetric
  from: number
  to: number
  rangeHours: number
  latencyLabel: string
  qualityLabel: string
}

export function NetworkHistoryChart({
  data,
  series,
  metric,
  from,
  to,
  rangeHours,
  latencyLabel,
  qualityLabel,
}: HistoryChartProps) {
  const { locale } = useLocale()
  return (
    <div className="h-80 w-full" data-testid="network-history-chart" data-from={from} data-to={to}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 8 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="timestamp"
            type="number"
            scale="linear"
            domain={[from, to]}
            allowDataOverflow
            tickFormatter={(value) => formatHistoryTime(value, rangeHours, locale)}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
          />
          <YAxis
            tickFormatter={(value) => metric === "latency" ? formatLatency(Number(value)) : formatPercent(Number(value))}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
            width={56}
          />
          <Tooltip
            labelFormatter={(value) => formatDateTime(locale, Number(value) * 1000)}
            formatter={(value, name) => [
              metric === "latency" ? formatLatency(Number(value)) : formatPercent(Number(value)),
              String(name),
            ]}
            contentStyle={{
              fontSize: "12px",
              borderRadius: "8px",
              border: "1px solid var(--border)",
            }}
          />
          <Legend wrapperStyle={{ fontSize: 12 }} />
          {series.map((item) => (
            <Line
              key={item.key}
              type="monotone"
              dataKey={metric === "latency" ? `latency-${item.key}` : `quality-${item.key}`}
              name={item.label}
              stroke={item.color}
              strokeWidth={2}
              dot={false}
              connectNulls={false}
            />
          ))}
          <text x={8} y={12} className="fill-muted-foreground" fontSize={10}>
            {metric === "latency" ? latencyLabel : qualityLabel}
          </text>
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

export function buildChartData(series: NetworkHistorySeries[], metric: ChartMetric, qualityKind: "loss" | "success") {
  const rows = new Map<number, Record<string, number | null>>()
  const chartSeries: ChartSeries[] = []
  series.forEach((item, index) => {
    const key = String(index)
    chartSeries.push({
      key,
      label: item.label || item.region || item.target_id,
      carrier: item.carrier,
      color: carrierColor(item.carrier),
    })
    for (const point of item.points) {
      const timestamp = Math.floor(Date.parse(point.timestamp) / 1000)
      if (!Number.isFinite(timestamp)) continue
      const row = rows.get(timestamp) ?? { timestamp }
      const failed = typeof point.success_percent === "number" && point.success_percent <= 0
      const latency = !failed && Number.isFinite(point.average_latency_ms) && point.average_latency_ms >= 0
        ? point.average_latency_ms
        : null
      const quality = metric === "latency"
        ? latency
        : itemCarrierQuality(point, qualityKind)
      row[`latency-${key}`] = latency
      row[`quality-${key}`] = quality
      rows.set(timestamp, row)
    }
  })
  return {
    data: [...rows.values()].sort((left, right) => Number(left.timestamp) - Number(right.timestamp)),
    series: chartSeries,
  }
}

function itemCarrierQuality(
  point: NetworkHistorySeries["points"][number],
  qualityKind: "loss" | "success",
): number | null {
  if (qualityKind === "loss" && typeof point.loss_percent === "number") return point.loss_percent
  if (qualityKind === "success" && typeof point.success_percent === "number") return point.success_percent
  return null
}

function describeTask(view: NetworkTaskView | null): string {
  if (!view) return ""
  const targets = view.task.targets ?? []
  if (targets.length === 0) return view.task.target
  return targets.map((target) => [target.label, target.region].filter(Boolean).join(" · ")).filter(Boolean).join(" | ")
}

export function formatHistoryTime(value: number | string, rangeHours: number, locale?: string): string {
  const date = new Date(Number(value) * 1000)
  if (rangeHours >= 168) {
    return dateTimeFormatter(locale, { month: "numeric", day: "numeric", hour: "2-digit" }).format(date)
  }
  return dateTimeFormatter(locale, { hour: "2-digit", minute: "2-digit" }).format(date)
}
