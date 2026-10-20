import { preloadNodeDetail } from "@/lib/route-loaders"
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"
import { Link } from "react-router"
import { Badge } from "@/components/ui/badge"
import {
  Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { AcceleratorSummary } from "@/components/accelerator-summary"
import { PublicNodeMetadata } from "@/components/public-node-metadata"
import { TrafficUsage } from "@/components/traffic-usage"
import { useLocale } from "@/context/locale"
import { formatInteger } from "@/lib/intl-format"
import { TelemetryStatus } from "@/components/telemetry-status"
import { formatBytes, formatDuration, formatMetricValue } from "@/lib/metric-format"
import { cn } from "@/lib/utils"
import type { Node } from "@/types"

interface NodeCardProps {
  node?: Node
  loading?: boolean
}

export function NodeCard({ node, loading = false }: NodeCardProps) {
  const { locale, t } = useLocale()
  if (loading || !node) return <NodeCardSkeleton />

  const metrics = node.metrics ?? {}
  const displayName = node.alias || node.name
  const isOnline = node.status === "online"

  return (
    <Link to={`/node/${encodeURIComponent(node.id)}`} onMouseEnter={preloadNodeDetail} onFocus={preloadNodeDetail} className="block h-full min-w-0">
      <Card size="sm" className="h-full min-w-0 transition-shadow hover:shadow-md">
        <CardHeader>
          <CardTitle className="truncate" title={displayName}>{displayName}</CardTitle>
          <CardDescription className="min-w-0">
            <p className="truncate" title={formatSystem(node)}>{formatSystem(node)}</p>
            <p className="truncate text-xs" title={node.cpu_model}>{node.cpu_model || node.host}</p>
          </CardDescription>
          <CardAction>
            <Badge variant={isOnline ? "default" : "secondary"} className="gap-1">
              <span className={cn("size-2 rounded-full", isOnline ? "bg-primary-foreground" : "bg-muted-foreground")} />
              {isOnline ? t("node.online") : t("node.offline")}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-1 flex-col gap-3">
          <div><TelemetryStatus node={node} /></div>
          <PublicNodeMetadata node={node} />
          <AcceleratorSummary devices={node.accelerators} />
          {(node.tags?.length ?? 0) > 0 ? (
            <div className="flex min-w-0 flex-wrap gap-1.5">
              {node.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}
            </div>
          ) : null}
          {node.public_remark ? (
            <p className="line-clamp-2 text-xs leading-relaxed text-muted-foreground" title={node.public_remark}>
              {node.public_remark}
            </p>
          ) : null}
          <dl className="flex flex-col gap-2.5">
            <ResourceRow
              label={t("node.cpu")}
              value={formatCPU(metrics.cpu_used, metrics.cpu_total, { unit: t("node.cores"), locale })}
              detail={formatPercent(metrics.cpu, locale)}
            />
            <ResourceRow
              label={t("node.memory")}
              value={formatCapacity(metrics.memory_used, metrics.memory_total, locale)}
              detail={formatPercent(metrics.memory, locale)}
            />
            <ResourceRow
              label={t("node.disk")}
              value={formatCapacity(metrics.disk_used, metrics.disk_total, locale)}
              detail={formatPercent(metrics.disk, locale)}
            />
            <ResourceRow
              label={t("node.swap")}
              value={formatCapacity(metrics.swap_used, metrics.swap_total, locale)}
              detail={formatPercent(metrics.swap, locale)}
            />
          </dl>
          <dl className="grid grid-cols-1 gap-2 border-t pt-3 sm:grid-cols-2">
            <NetworkRow
              label={t("node.network")}
              sent={formatRate(metrics.net_sent, locale)}
              received={formatRate(metrics.net_recv, locale)}
            />
            <NetworkRow
              label={t("node.traffic")}
              sent={formatTotal(metrics.net_sent_total, locale)}
              received={formatTotal(metrics.net_recv_total, locale)}
            />
          </dl>
          <TrafficUsage traffic={node.traffic} className="border-t pt-3" />
        </CardContent>
        <CardFooter className="grid grid-cols-2 gap-x-4 gap-y-2">
          <CompactStat label={t("node.uptime")} value={formatOptionalDuration(metrics.uptime, locale)} />
          <CompactStat label={t("node.load")} value={formatLoad(metrics, locale)} />
          <CompactStat label={t("node.processes")} value={formatCount(metrics.processes, locale)} />
          <CompactStat label={t("node.connections")} value={formatConnections(metrics, locale)} />
        </CardFooter>
      </Card>
    </Link>
  )
}

function ResourceRow({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="grid min-w-0 grid-cols-[5rem_1fr_auto] items-baseline gap-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate font-medium tabular-nums" title={value}>{value}</dd>
      <dd className="text-xs text-muted-foreground tabular-nums">{detail}</dd>
    </div>
  )
}

function NetworkRow({ label, sent, received }: { label: string; sent: string; received: string }) {
  return (
    <div className="min-w-0">
      <dt className="mb-1 text-xs text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-3 text-xs tabular-nums">
        <span className="flex min-w-0 items-center gap-1"><ArrowUpIcon className="size-3" />{sent}</span>
        <span className="flex min-w-0 items-center gap-1"><ArrowDownIcon className="size-3" />{received}</span>
      </dd>
    </div>
  )
}

function CompactStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-xs font-medium tabular-nums" title={value}>{value}</dd>
    </div>
  )
}

function formatSystem(node: Node): string {
  const system = [node.platform || node.os, node.os_version].filter(Boolean).join(" ")
  return [system, node.arch].filter(Boolean).join(" / ") || node.host
}

function formatPercent(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatMetricValue(value, "percent", locale)
}

function formatCPU(used: number | undefined, total: number | undefined, options: { unit: string; locale?: string }): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatNumber(used, options.locale)} / ${formatNumber(total, options.locale)} ${options.unit}`
}

function formatCapacity(used: number | undefined, total: number | undefined, locale?: string): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatBytes(used, locale)} / ${formatBytes(total, locale)}`
}

function formatRate(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatMetricValue(value, "bytes-per-second", locale)
}

function formatTotal(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatBytes(value, locale)
}

function formatOptionalDuration(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatDuration(value, locale)
}

function formatLoad(metrics: Record<string, number>, locale?: string): string {
  if (metrics.load1 === undefined) return "--"
  return [metrics.load1, metrics.load5, metrics.load15].map((value) => formatNumber(value, locale)).join(" / ")
}

function formatCount(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatInteger(locale, value)
}

function formatConnections(metrics: Record<string, number>, locale?: string): string {
  if (metrics.tcp_connections === undefined || metrics.udp_connections === undefined) return "--"
  return `${formatCount(metrics.tcp_connections, locale)} TCP / ${formatCount(metrics.udp_connections, locale)} UDP`
}

function formatNumber(value: number, locale?: string): string {
  return formatMetricValue(value, "number", locale)
}

function NodeCardSkeleton() {
  return (
    <Card size="sm" className="h-full">
      <CardHeader>
        <CardTitle><Skeleton className="h-4 w-32" /></CardTitle>
        <CardDescription><Skeleton className="h-8 w-40" /></CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-5 w-full" />)}
        <Skeleton className="h-10 w-full" />
      </CardContent>
      <CardFooter className="grid grid-cols-2 gap-2">
        {Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-7 w-full" />)}
      </CardFooter>
    </Card>
  )
}
