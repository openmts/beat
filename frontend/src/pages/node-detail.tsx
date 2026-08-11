import { useState } from "react"
import { ArrowLeftIcon } from "lucide-react"
import { Link, useParams } from "react-router"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { MetricsChart } from "@/components/metrics-chart"
import { TrafficUsage } from "@/components/traffic-usage"
import { useLocale } from "@/context/locale"
import { useNode, useNodeMetrics } from "@/hooks/use-api"
import { formatBytes, formatDuration, formatMetricValue } from "@/lib/metric-format"
import type { Node } from "@/types"

type TimeRange = "1h" | "6h" | "24h" | "7d"

const RANGE_HOURS: Record<TimeRange, number> = {
  "1h": 1,
  "6h": 6,
  "24h": 24,
  "7d": 168,
}

const CHART_CONFIGS = [
  { key: "cpu", titleKey: "metrics.cpu", color: "#2563eb", valueFormat: "percent" },
  { key: "memory", titleKey: "metrics.memory", color: "#059669", valueFormat: "percent" },
  { key: "disk", titleKey: "metrics.disk", color: "#d97706", valueFormat: "percent" },
  { key: "swap", titleKey: "metrics.swap", color: "#7c3aed", valueFormat: "percent" },
  { key: "load1", titleKey: "metrics.load", color: "#e11d48", valueFormat: "number" },
  { key: "processes", titleKey: "metrics.processes", color: "#0891b2", valueFormat: "number" },
  { key: "tcp_connections", titleKey: "metrics.connections", color: "#4f46e5", valueFormat: "number" },
  { key: "disk_read", titleKey: "metrics.disk_read", color: "#ca8a04", valueFormat: "bytes-per-second" },
  { key: "disk_write", titleKey: "metrics.disk_write", color: "#dc2626", valueFormat: "bytes-per-second" },
  { key: "net_recv", titleKey: "metrics.net_recv", color: "#9333ea", valueFormat: "bytes-per-second" },
  { key: "net_sent", titleKey: "metrics.net_sent", color: "#0284c7", valueFormat: "bytes-per-second" },
] as const

function NodeDetail() {
  const { t } = useLocale()
  const { id } = useParams<{ id: string }>()
  const [timeRange, setTimeRange] = useState<TimeRange>("1h")
  const { data: node, loading: nodeLoading, error: nodeError } = useNode(id)
  const hours = RANGE_HOURS[timeRange]
  const metricsState = useNodeMetrics(id, undefined, hours)

  return (
    <div className="flex min-h-screen flex-col">
      <Header />
      <header className="sticky top-12 z-20 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-4 py-2 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
        <Link
          to="/"
          aria-label={t("app.back")}
          title={t("app.back")}
          className={buttonVariants({ variant: "ghost", size: "icon" })}
        >
          <ArrowLeftIcon />
        </Link>
        <h1 className="min-w-0 flex-1 truncate text-lg font-bold">
          {nodeLoading ? t("node.loading") : node ? node.alias || node.name : t("app.nodes")}
        </h1>
        {node ? (
          <Badge variant={node.status === "online" ? "default" : "secondary"}>
            {node.status === "online" ? t("node.online") : t("node.offline")}
          </Badge>
        ) : null}
      </header>
      <main className="container-page safe-pb flex-1 py-4 sm:py-6">
        <ErrorAlerts nodeError={nodeError} metricsError={metricsState.error} />
        {nodeLoading ? <Skeleton className="mb-6 h-44 w-full" /> : node ? <NodeOverview node={node} /> : null}
        <div className="mb-4 flex justify-end">
          <ToggleGroup
            value={[timeRange]}
            onValueChange={(value: string[]) => {
              if (value.length > 0) setTimeRange(value[0] as TimeRange)
            }}
          >
            <ToggleGroupItem value="1h">1h</ToggleGroupItem>
            <ToggleGroupItem value="6h">6h</ToggleGroupItem>
            <ToggleGroupItem value="24h">24h</ToggleGroupItem>
            <ToggleGroupItem value="7d">7d</ToggleGroupItem>
          </ToggleGroup>
        </div>
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
          {CHART_CONFIGS.map((config) => metricsState.loading ? (
            <div key={config.key} className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">{t(config.titleKey)}</h3>
              <Skeleton className="h-48 w-full" />
            </div>
          ) : (
            <MetricsChart
              key={config.key}
              data={metricsState.data?.[config.key]}
              title={t(config.titleKey)}
              color={config.color}
              valueFormat={config.valueFormat}
              from={metricsState.from}
              to={metricsState.to}
              rangeHours={hours}
            />
          ))}
        </div>
      </main>
      <PublicFooter />
    </div>
  )
}

function ErrorAlerts({ nodeError, metricsError }: { nodeError: string | null; metricsError: string | null }) {
  const errors = [nodeError, metricsError].filter((error): error is string => error !== null)
  return errors.length > 0 ? (
    <div className="mb-4 flex flex-col gap-2">
      {errors.map((error) => (
        <Alert key={error} variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
      ))}
    </div>
  ) : null
}

function NodeOverview({ node }: { node: Node }) {
  const { t } = useLocale()
  const metrics = node.metrics ?? {}
  const items = [
    [t("node.system"), formatSystem(node)],
    [t("node.kernel"), node.kernel || "--"],
    [t("node.cpu_model"), node.cpu_model || "--"],
    [t("node.virtualization"), node.virtualization || "--"],
    [t("node.agent"), node.agent_version || "--"],
    [t("node.uptime"), valueOr(metrics.uptime, formatDuration)],
    [t("node.load"), formatLoad(metrics)],
    [t("node.processes"), formatCount(metrics.processes)],
    [t("node.connections"), formatConnections(metrics)],
    [t("node.traffic"), formatTraffic(metrics)],
    [t("node.last_seen"), node.last_seen ? new Date(node.last_seen).toLocaleString() : "--"],
  ]
  return (
    <section className="mb-6 border-y bg-muted/20 py-4" aria-label={t("node.metrics")}>
      {(node.tags?.length ?? 0) > 0 || node.public_remark ? (
        <div className="mb-4 flex flex-col gap-2 px-1">
          {(node.tags?.length ?? 0) > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {node.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}
            </div>
          ) : null}
          {node.public_remark ? <p className="text-sm text-muted-foreground">{node.public_remark}</p> : null}
        </div>
      ) : null}
      <TrafficUsage traffic={node.traffic} className="mb-4 px-1" />
      <dl className="grid grid-cols-2 gap-x-6 gap-y-4 px-1 sm:grid-cols-3 lg:grid-cols-6">
        {items.map(([label, value]) => <OverviewItem key={label} label={label} value={value} />)}
      </dl>
    </section>
  )
}

function OverviewItem({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="break-words font-medium tabular-nums">{value}</dd></div>
}

function formatSystem(node: Node): string {
  const name = [node.platform || node.os, node.os_version].filter(Boolean).join(" ")
  return [name, node.arch].filter(Boolean).join(" / ") || "--"
}

function valueOr(value: number | undefined, formatter: (value: number) => string): string {
  return value === undefined ? "--" : formatter(value)
}

function formatLoad(metrics: Record<string, number>): string {
  if (metrics.load1 === undefined) return "--"
  return [metrics.load1, metrics.load5, metrics.load15].map((value) => formatMetricValue(value, "number")).join(" / ")
}

function formatCount(value: number | undefined): string {
  return value === undefined ? "--" : Math.round(value).toLocaleString()
}

function formatConnections(metrics: Record<string, number>): string {
  if (metrics.tcp_connections === undefined || metrics.udp_connections === undefined) return "--"
  return `${formatCount(metrics.tcp_connections)} TCP / ${formatCount(metrics.udp_connections)} UDP`
}

function formatTraffic(metrics: Record<string, number>): string {
  if (metrics.net_sent_total === undefined || metrics.net_recv_total === undefined) return "--"
  return `UP ${formatBytes(metrics.net_sent_total)} / DOWN ${formatBytes(metrics.net_recv_total)}`
}

export default NodeDetail
