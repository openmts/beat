import { useEffect, useMemo, useState } from "react"
import {
  CartesianGrid,
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
import { formatLatency, networkHistoryRanges, networkNodeLabel } from "@/lib/network-quality"
import type { NetworkTaskView } from "@/types"

interface NetworkHistoryDialogProps {
  open: boolean
  view: NetworkTaskView | null
  initialNodeId?: string
  admin?: boolean
  onOpenChange: (open: boolean) => void
}

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

  useEffect(() => {
    if (!open) return
    setNodeId(initialNodeId ?? view?.nodes[0]?.node.id ?? "")
    setRangeHours(24)
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
  const chartData = useMemo(() => history.data?.points.map((point) => ({
    timestamp: new Date(point.timestamp).getTime() / 1000,
    latency: point.average_latency_ms,
    success: point.success_percent,
  })) ?? [], [history.data])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{view?.task.name ?? t("network.history")}</DialogTitle>
          <DialogDescription>{view?.task.target}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <Select items={nodeOptions} value={nodeId} onValueChange={(value) => setNodeId(value ?? "")}>
              <SelectTrigger aria-label={t("network.node")} className="w-full sm:w-64">
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
                <ToggleGroupItem
                  key={range.hours}
                  value={String(range.hours)}
                  aria-label={range.label}
                >
                  {range.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
          {history.error ? (
            <Alert variant="destructive"><AlertDescription>{history.error}</AlertDescription></Alert>
          ) : history.loading ? (
            <Skeleton className="h-72 w-full" />
          ) : chartData.length === 0 ? (
            <div className="flex h-72 items-center justify-center border border-dashed text-sm text-muted-foreground">
              {t("network.no_history")}
            </div>
          ) : (
            <NetworkHistoryChart
              data={chartData}
              from={history.from}
              to={history.to}
              rangeHours={rangeHours}
              latencyLabel={t("network.latency")}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface HistoryChartProps {
  data: Array<{ timestamp: number; latency: number; success: number }>
  from: number
  to: number
  rangeHours: number
  latencyLabel: string
}

export function NetworkHistoryChart({ data, from, to, rangeHours, latencyLabel }: HistoryChartProps) {
  return (
    <div className="h-72 w-full" data-testid="network-history-chart" data-from={from} data-to={to}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 8 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="timestamp"
            type="number"
            scale="linear"
            domain={[from, to]}
            allowDataOverflow
            tickFormatter={(value) => formatHistoryTime(value, rangeHours)}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
          />
          <YAxis
            tickFormatter={(value) => formatLatency(Number(value))}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            className="fill-muted-foreground"
            width={56}
          />
          <Tooltip
            labelFormatter={(value) => new Date(Number(value) * 1000).toLocaleString()}
            formatter={(value) => [formatLatency(Number(value)), latencyLabel]}
            contentStyle={{
              fontSize: "12px",
              borderRadius: "8px",
              border: "1px solid var(--border)",
            }}
          />
          <Line type="monotone" dataKey="latency" stroke="var(--primary)" strokeWidth={2} dot={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

export function formatHistoryTime(value: number | string, rangeHours: number): string {
  const date = new Date(Number(value) * 1000)
  if (rangeHours >= 168) {
    return date.toLocaleString(undefined, { month: "numeric", day: "numeric", hour: "2-digit" })
  }
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
}
