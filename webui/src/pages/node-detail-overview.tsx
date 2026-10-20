import { CpuIcon, HardDriveIcon, MemoryStickIcon, RefreshCwIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { PublicNodeMetadata } from "@/components/public-node-metadata"
import { TrafficUsage } from "@/components/traffic-usage"
import { TelemetryStatus } from "@/components/telemetry-status"
import { usePublicTheme } from "@/lib/theme-presentation"
import { useLocale } from "@/context/locale"
import { formatDuration } from "@/lib/metric-format"
import type { Node } from "@/types"
import { formatCapacity, formatSystem, formatLastSeen, valueOr, formatLoad, formatCount, formatConnections, formatTraffic, formatNetwork } from "@/pages/node-detail-formatters"

export function NodeOverview({ node }: { node: Node }) {
  const metrics = node.metrics ?? {}
  return <div className="flex flex-col gap-4">
    <div><TelemetryStatus node={node} /></div><PublicNodeMetadata node={node} /><NodeNotes node={node} /><NodeResources metrics={metrics} /><NodeSystem node={node} /><RuntimeStats metrics={metrics} />
  </div>
}
function NodeNotes({ node }: { node: Node }) {
  return (node.tags?.length || node.public_remark) ? <div className="flex flex-col gap-2">
    {node.tags?.length ? <div className="flex flex-wrap gap-1.5">{node.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}</div> : null}
    {node.public_remark ? <p className="text-sm text-muted-foreground">{node.public_remark}</p> : null}
  </div> : null
}
function NodeResources({ metrics }: { metrics: Record<string, number> }) {
  const { locale, t } = useLocale()
  const layout = usePublicTheme()?.definition.detail ?? "balanced"
  return (<div className={layout === "compact" ? "grid grid-cols-2 gap-3" : layout === "spotlight" ? "grid grid-cols-2 gap-4 lg:grid-cols-[2fr_1fr_1fr_1fr]" : "grid grid-cols-2 gap-3 sm:grid-cols-4"}>
        <ResourceCard
          icon={CpuIcon}
          label={t("node.cpu")}
          percent={metrics.cpu}
          value={formatCapacity(metrics.cpu_used, metrics.cpu_total, { unit: t("node.cores"), locale })}
        />
        <ResourceCard
          icon={MemoryStickIcon}
          label={t("node.memory")}
          percent={metrics.memory}
          value={formatCapacity(metrics.memory_used, metrics.memory_total, { locale })}
        />
        <ResourceCard
          icon={HardDriveIcon}
          label={t("node.disk")}
          percent={metrics.disk}
          value={formatCapacity(metrics.disk_used, metrics.disk_total, { locale })}
        />
        <ResourceCard
          icon={RefreshCwIcon}
          label={t("node.swap")}
          percent={metrics.swap}
          value={formatCapacity(metrics.swap_used, metrics.swap_total, { locale })}
        />
      </div>)
}
function NodeSystem({ node }: { node: Node }) {
  const { locale, t } = useLocale()
  const compact = usePublicTheme()?.definition.detail === "compact"
  return (<div className={compact ? "grid grid-cols-1 gap-4" : "grid grid-cols-1 gap-4 lg:grid-cols-[1fr_22rem]"}>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{t("node.system")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
              <InfoItem label={t("node.system")} value={formatSystem(node)} />
              <InfoItem label={t("node.kernel")} value={node.kernel || "--"} />
              <InfoItem label={t("node.cpu_model")} value={node.cpu_model || "--"} />
              <InfoItem label={t("node.virtualization")} value={node.virtualization || "--"} />
              <InfoItem label={t("node.agent")} value={node.agent_version || "--"} />
              <InfoItem label={t("node.last_seen")} value={formatLastSeen(node.last_seen, locale)} />
            </dl>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{t("traffic.current_cycle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <TrafficUsage traffic={node.traffic} />
          </CardContent>
        </Card>
      </div>)
}
function ResourceCard({ icon: Icon, label, percent, value }: {
  icon: typeof CpuIcon
  label: string
  percent: number | undefined
  value: string
}) {
  const pct = percent === undefined ? null : Math.max(0, Math.min(percent, 100))
  return (
    <Card>
      <CardContent className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <Icon className="size-4 shrink-0 text-muted-foreground" />
          <span className="text-xs font-medium text-muted-foreground">{label}</span>
        </div>
        <span className="text-2xl font-semibold tabular-nums">
          {pct === null ? "--" : `${pct.toFixed(1)}%`}
        </span>
        <span className="truncate text-xs text-muted-foreground tabular-nums" title={value}>{value}</span>
        {pct !== null ? (
          <Progress value={pct} aria-label={`${label} ${pct.toFixed(1)}%`} />
        ) : null}
      </CardContent>
    </Card>
  )
}

function InfoItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 truncate text-sm" title={value}>{value}</dd>
    </div>
  )
}

function RuntimeStats({ metrics }: { metrics: Record<string, number> }) {
  const { locale, t } = useLocale()
  const items = [
    [t("node.uptime"), valueOr(metrics.uptime, (value) => formatDuration(value, locale))],
    [t("node.load"), formatLoad(metrics, locale)],
    [t("node.processes"), formatCount(metrics.processes, locale)],
    [t("node.connections"), formatConnections(metrics, locale)],
    [t("node.traffic"), formatTraffic(metrics, locale)],
    [t("node.network"), formatNetwork(metrics, locale)],
  ]
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-3 border-t pt-4 sm:grid-cols-3">
      {items.map(([label, value]) => (
        <InfoItem key={label} label={label} value={value} />
      ))}
    </dl>
  )
}
