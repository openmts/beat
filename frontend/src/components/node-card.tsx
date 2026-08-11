import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"
import { Link } from "react-router"
import { Badge } from "@/components/ui/badge"
import {
  Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { TrafficUsage } from "@/components/traffic-usage"
import { useLocale } from "@/context/locale"
import { formatBytes, formatDuration, formatMetricValue } from "@/lib/metric-format"
import { cn } from "@/lib/utils"
import type { Node } from "@/types"

interface NodeCardProps {
  node?: Node
  loading?: boolean
}

export function NodeCard({ node, loading = false }: NodeCardProps) {
  const { t } = useLocale()
  if (loading || !node) return <NodeCardSkeleton />

  const metrics = node.metrics ?? {}
  const displayName = node.alias || node.name
  const isOnline = node.status === "online"

  return (
    <Link to={`/node/${node.id}`} className="block h-full min-w-0">
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
              value={formatCPU(metrics.cpu_used, metrics.cpu_total, t("node.cores"))}
              detail={formatPercent(metrics.cpu)}
            />
            <ResourceRow
              label={t("node.memory")}
              value={formatCapacity(metrics.memory_used, metrics.memory_total)}
              detail={formatPercent(metrics.memory)}
            />
            <ResourceRow
              label={t("node.disk")}
              value={formatCapacity(metrics.disk_used, metrics.disk_total)}
              detail={formatPercent(metrics.disk)}
            />
            <ResourceRow
              label={t("node.swap")}
              value={formatCapacity(metrics.swap_used, metrics.swap_total)}
              detail={formatPercent(metrics.swap)}
            />
          </dl>
          <dl className="grid grid-cols-1 gap-2 border-t pt-3 sm:grid-cols-2">
            <NetworkRow
              label={t("node.network")}
              sent={formatRate(metrics.net_sent)}
              received={formatRate(metrics.net_recv)}
            />
            <NetworkRow
              label={t("node.traffic")}
              sent={formatTotal(metrics.net_sent_total)}
              received={formatTotal(metrics.net_recv_total)}
            />
          </dl>
          <TrafficUsage traffic={node.traffic} className="border-t pt-3" />
        </CardContent>
        <CardFooter className="grid grid-cols-2 gap-x-4 gap-y-2">
          <CompactStat label={t("node.uptime")} value={formatOptionalDuration(metrics.uptime)} />
          <CompactStat label={t("node.load")} value={formatLoad(metrics)} />
          <CompactStat label={t("node.processes")} value={formatCount(metrics.processes)} />
          <CompactStat label={t("node.connections")} value={formatConnections(metrics)} />
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

function formatPercent(value: number | undefined): string {
  return value === undefined ? "--" : formatMetricValue(value, "percent")
}

function formatCPU(used: number | undefined, total: number | undefined, unit: string): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatNumber(used)} / ${formatNumber(total)} ${unit}`
}

function formatCapacity(used: number | undefined, total: number | undefined): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatBytes(used)} / ${formatBytes(total)}`
}

function formatRate(value: number | undefined): string {
  return value === undefined ? "--" : formatMetricValue(value, "bytes-per-second")
}

function formatTotal(value: number | undefined): string {
  return value === undefined ? "--" : formatBytes(value)
}

function formatOptionalDuration(value: number | undefined): string {
  return value === undefined ? "--" : formatDuration(value)
}

function formatLoad(metrics: Record<string, number>): string {
  if (metrics.load1 === undefined) return "--"
  return [metrics.load1, metrics.load5, metrics.load15].map(formatNumber).join(" / ")
}

function formatCount(value: number | undefined): string {
  return value === undefined ? "--" : Math.round(value).toLocaleString()
}

function formatConnections(metrics: Record<string, number>): string {
  if (metrics.tcp_connections === undefined || metrics.udp_connections === undefined) return "--"
  return `${formatCount(metrics.tcp_connections)} TCP / ${formatCount(metrics.udp_connections)} UDP`
}

function formatNumber(value: number): string {
  return value.toFixed(1).replace(/\.0$/, "")
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
