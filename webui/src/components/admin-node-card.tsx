import { PencilIcon } from "lucide-react"
import { AdminNodeActions, AdminNodePresentation } from "@/components/admin-node-presentation"
import { useLocale } from "@/context/locale"
import type { ManagedNode } from "@/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { TrafficUsage } from "@/components/traffic-usage"
import { formatBytes, formatDuration, formatMetricValue } from "@/lib/metric-format"
import { cn } from "@/lib/utils"

interface AdminNodeCardProps {
  node: ManagedNode
  sshKeyName: string
  onEdit: () => void
  onDelete: () => void
  onInstall: () => void
  onRevoke: () => void
  onRotate: () => void
  canMoveUp: boolean
  canMoveDown: boolean
  onMoveUp: () => void
  onMoveDown: () => void
}

export function AdminNodeCard({
  node,
  sshKeyName,
  onEdit,
  onDelete,
  onInstall,
  onRevoke,
  onRotate,
  canMoveUp,
  canMoveDown,
  onMoveUp,
  onMoveDown,
}: AdminNodeCardProps) {
  const { t } = useLocale()
  const displayName = node.alias || node.name
  const endpoint = `${node.host}:${node.port}`
  const metrics = node.metrics ?? {}
  const system = formatSystem(node)

  return (
    <Card size="sm" className="h-full min-w-0" data-testid={`node-card-${node.id}`}>
      <CardHeader>
        <CardTitle className="truncate" title={displayName}>{displayName}</CardTitle>
        <CardDescription className="min-h-5 truncate" title={node.name}>
          {node.alias ? node.name : null}
        </CardDescription>
        <CardAction>
          <Badge variant={node.status === "online" ? "default" : "secondary"}>
            {node.status === "online" ? t("node.online") : t("node.offline")}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3">
        <AdminNodePresentation node={node} />
        <dl className="flex min-w-0 flex-col gap-3">
          <div className="min-w-0">
            <dt className="text-xs text-muted-foreground">{t("node.host")}</dt>
            <dd className="max-w-full break-all font-mono text-sm" title={endpoint}>{endpoint}</dd>
          </div>
	        <div className="min-w-0">
	          <dt className="text-xs text-muted-foreground">{t("node.system")}</dt>
	          <dd className="truncate text-sm" title={system}>{system}</dd>
	        </div>
          <div className="min-w-0">
            <dt className="text-xs text-muted-foreground">{t("node.ssh_key")}</dt>
            <dd className="truncate text-sm" title={sshKeyName}>{sshKeyName}</dd>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="min-w-0">
              <dt className="text-xs text-muted-foreground">{t("node.agent")}</dt>
              <dd className="truncate text-sm" title={node.agent_version || "--"}>
                {node.agent_version || "--"}
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="text-xs text-muted-foreground">{t("agent.credential")}</dt>
              <dd>
                <Badge variant={credentialBadgeVariant(node.agent_credential_status)}>
                  {t(`agent.status.${node.agent_credential_status}`)}
                </Badge>
              </dd>
            </div>
          </div>
        </dl>
        <dl className="grid min-w-0 grid-cols-2 gap-x-3 gap-y-3 border-t pt-3">
          <MetricItem
            label={t("node.cpu")}
            value={formatCPUUsage(metrics.cpu_used, metrics.cpu_total, t("node.cores"))}
            detail={formatPercent(metrics.cpu)}
          />
          <MetricItem
            label={t("node.memory")}
            value={formatCapacityUsage(metrics.memory_used, metrics.memory_total)}
            detail={formatPercent(metrics.memory)}
          />
          <MetricItem
            className="col-span-2"
            label={t("node.disk")}
            value={formatDiskUsage(metrics.disk_used, metrics.disk_total)}
	          detail={formatPercent(metrics.disk)}
          />
	        <MetricItem
	          label={t("node.swap")}
	          value={formatCapacityUsage(metrics.swap_used, metrics.swap_total)}
	          detail={formatPercent(metrics.swap)}
	        />
	        <MetricItem
	          label={t("node.uptime")}
	          value={metrics.uptime === undefined ? "--" : formatDuration(metrics.uptime)}
	          detail={formatLoad(metrics)}
	        />
	        <MetricItem
	          label={t("node.network")}
	          value={formatNetworkRate(metrics.net_sent, metrics.net_recv)}
	        />
	        <MetricItem
	          label={t("node.traffic")}
	          value={formatTraffic(metrics.net_sent_total, metrics.net_recv_total)}
	        />
        </dl>
        <TrafficUsage traffic={node.traffic} className="border-t pt-3" />
      </CardContent>
      <CardFooter className="justify-end gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t("app.edit")}
          title={t("app.edit")}
          onClick={onEdit}
        >
          <PencilIcon />
        </Button>
        <AdminNodeActions
          canMoveUp={canMoveUp}
          canMoveDown={canMoveDown}
          onMoveUp={onMoveUp}
          onMoveDown={onMoveDown}
          onInstall={onInstall}
          onRotate={onRotate}
          onRevoke={onRevoke}
          onDelete={onDelete}
        />
      </CardFooter>
    </Card>
  )
}

export function AdminNodeCardSkeleton() {
  return (
    <Card size="sm" className="h-full" data-testid="node-card-skeleton">
      <CardHeader>
        <CardTitle><Skeleton className="h-4 w-28" /></CardTitle>
        <CardDescription><Skeleton className="h-3 w-20" /></CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-3/4" />
        <div className="grid grid-cols-2 gap-3 border-t pt-3">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="col-span-2 h-9 w-full" />
        </div>
      </CardContent>
      <CardFooter className="justify-end gap-1">
        <Skeleton className="size-7" />
        <Skeleton className="size-7" />
      </CardFooter>
    </Card>
  )
}

interface MetricItemProps {
  className?: string
  detail?: string
  label: string
  value: string
}

function MetricItem({ className, detail, label, value }: MetricItemProps) {
  return (
    <div className={cn("min-w-0 border-l-2 border-muted pl-2", className)}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words font-medium tabular-nums">{value}</dd>
      {detail ? <dd className="text-xs text-muted-foreground tabular-nums">{detail}</dd> : null}
    </div>
  )
}

function formatPercent(value: number | undefined): string {
  return value === undefined ? "--" : formatMetricValue(value, "percent")
}

function formatCPUUsage(used: number | undefined, total: number | undefined, unit: string): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatNumber(used)} / ${formatNumber(total)} ${unit}`
}

function formatCapacityUsage(used: number | undefined, total: number | undefined): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatBytes(used)} / ${formatBytes(total)}`
}

function formatNumber(value: number): string {
  return value.toFixed(1).replace(/\.0$/, "")
}

function formatDiskUsage(used: number | undefined, total: number | undefined): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  return `${formatBytes(used)} / ${formatBytes(total)}`
}

function formatSystem(node: ManagedNode): string {
  const name = [node.platform || node.os, node.os_version].filter(Boolean).join(" ")
  return [name, node.arch].filter(Boolean).join(" / ") || "--"
}

function credentialBadgeVariant(status: ManagedNode["agent_credential_status"]) {
  if (status === "active") return "default"
  if (status === "revoked") return "destructive"
  return "secondary"
}

function formatLoad(metrics: Record<string, number>): string {
  if (metrics.load1 === undefined) return ""
  return [metrics.load1, metrics.load5, metrics.load15]
    .map((value) => value.toFixed(1).replace(/\.0$/, ""))
    .join(" / ")
}

function formatNetworkRate(sent: number | undefined, received: number | undefined): string {
  if (sent === undefined || received === undefined) return "--"
  return `UP ${formatMetricValue(sent, "bytes-per-second")} / DOWN ${formatMetricValue(received, "bytes-per-second")}`
}

function formatTraffic(sent: number | undefined, received: number | undefined): string {
  if (sent === undefined || received === undefined) return "--"
  return `UP ${formatBytes(sent)} / DOWN ${formatBytes(received)}`
}
