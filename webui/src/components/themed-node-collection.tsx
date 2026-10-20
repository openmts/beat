import { AcceleratorSummary } from "@/components/accelerator-summary"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
type GPUDevices = Record<string, AcceleratorDevicePublic[]>
import type { MouseEvent, ReactNode } from "react"
import { Link } from "react-router"
import { CpuIcon, MemoryStickIcon, HardDriveIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { TelemetryStatus } from "@/components/telemetry-status"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"
import { useLocale } from "@/context/locale"
import { themeFlag, themeResources, usePublicTheme } from "@/lib/theme-presentation"
import { formatMetricValue as formatPresentMetric, formatDuration } from "@/lib/metric-format"
import type { ThemeNode } from "@/types/theme-runtime"
import type { ThemeResource } from "@/types/theme-definition"

/** 每个资源行的图标与文案键；作者只能选择已知资源，不会出现未知内容。 */
const resourceSpec: Record<ThemeResource, { icon: typeof CpuIcon; label: string }> = {
  cpu: { icon: CpuIcon, label: "node.cpu" },
  memory: { icon: MemoryStickIcon, label: "node.memory" },
  disk: { icon: HardDriveIcon, label: "node.disk" },
}

export function ThemedNodeCollection({ selectedGroup, fleetLayout, hideGroupHeading = false, accelerators }: { accelerators?: GPUDevices; selectedGroup: string; fleetLayout?: "cards" | "table"; hideGroupHeading?: boolean }) {
  const theme = usePublicTheme()
  const { t } = useLocale()
  if (!theme) return null
  const nodes = theme.snapshot.nodes.filter((node) => !selectedGroup || node.group_id === selectedGroup)
  if (!nodes.length) return <p className="rounded-xl border border-dashed p-10 text-center text-muted-foreground">{t("node.empty")}</p>
  const layout = fleetLayout === "table" ? "table" : fleetLayout === "cards" && theme.definition.overview === "table" ? "cards" : theme.definition.overview
  return <div data-theme-overview={layout}>
    {layout === "table" ? <NodeOperationsTable nodes={nodes} accelerators={accelerators} /> : layout === "list" ?
      <NodeStatusList nodes={nodes} accelerators={accelerators} /> : layout === "tiles" ?
      <div className={`grid grid-cols-1 lg:grid-cols-2 ${theme.definition.density === "compact" ? "gap-3" : "gap-6"}`}>{nodes.map((node) => <NodeTile key={node.id} node={node} devices={accelerators?.[node.id]} />)}</div> :
      <GroupedNodeCards nodes={nodes} hideGroupHeading={hideGroupHeading} accelerators={accelerators} />}
  </div>
}

function GroupedNodeCards({ nodes, hideGroupHeading, accelerators }: { nodes: ThemeNode[]; hideGroupHeading?: boolean; accelerators?: GPUDevices }) {
  const theme = usePublicTheme()!
  const { t } = useLocale()
  const groups = new Map(theme.snapshot.groups.map((group) => [group.id, group.name]))
  const ids = [...new Set(nodes.map((node) => node.group_id))]
  return <div className="space-y-6">{ids.map((id) => <section key={id} className="space-y-3">
    {!hideGroupHeading && <div className="flex items-center gap-2"><h2 className="text-sm font-semibold">{groups.get(id) || t("theme.other_nodes")}</h2>
      <Badge variant="secondary">{nodes.filter((node) => node.group_id === id).length}</Badge></div>}
    <div className={`grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 ${theme.definition.density === "compact" ? "gap-3" : "gap-5"}`}>
      {nodes.filter((node) => node.group_id === id).map((node) => <NodeSummaryCard key={node.id} node={node} devices={accelerators?.[node.id]} />)}
    </div>
  </section>)}</div>
}

/**
 * 状态列表：一行一个节点，适合手机与告警列表。
 * 与表格不同，它不做表头对齐，只用状态圆点、内联资源读数与可选网络值保持紧凑。
 */
function NodeStatusList({ nodes, accelerators }: { nodes: ThemeNode[]; accelerators?: GPUDevices }) {
  const { t } = useLocale()
  const definition = usePublicTheme()!.definition
  const resources = themeResources(definition)
  const compact = definition.density === "compact"
  const network = themeFlag(definition, "show_network")
  return <ul className="divide-y overflow-hidden rounded-xl border lg:rounded-2xl">
    {nodes.map((node) => <li key={node.id} className={cn("flex flex-wrap items-center gap-x-4 gap-y-2 px-4", compact ? "py-2" : "py-3")}>
      <span aria-hidden="true" className={cn("size-2 shrink-0 rounded-full", node.status === "online" ? "bg-primary" : "bg-muted-foreground/50")} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2"><NodeLink node={node} /></div>
        {themeFlag(definition, "show_host") && node.host && <p className="truncate font-mono text-xs text-muted-foreground" title={node.host}>{node.host}</p>}
        {themeFlag(definition, "show_tags") && node.tags.length > 0 && <div className="mt-1 flex flex-wrap gap-1">{node.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}</div>}
        <AcceleratorSummary devices={accelerators?.[node.id]} />
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">{resources.map((resource) => <span key={resource} className="flex items-baseline gap-1.5 font-mono text-xs tabular-nums">
        <span className="text-muted-foreground">{t(resourceSpec[resource].label)}</span>{formatMetric(node.metrics[resource], "percent")}</span>)}
        {network && <span className="font-mono text-xs tabular-nums text-muted-foreground">↓ {formatMetric(node.metrics.net_recv, "bytes-per-second")} ↑ {formatMetric(node.metrics.net_sent, "bytes-per-second")}</span>}</div>
      <div className="flex items-center gap-2">{themeFlag(definition, "show_remark") && node.public_remark && <span className="hidden max-w-48 truncate text-xs text-muted-foreground lg:inline" title={node.public_remark}>{node.public_remark}</span>}<NodeStatus node={node} /></div>
    </li>)}
  </ul>
}

function NodeLink({ node, children }: { node: ThemeNode; children?: ReactNode }) {
  const theme = usePublicTheme()
  const select = (event: MouseEvent<HTMLAnchorElement>) => {
    if (theme?.onNodeSelect) { event.preventDefault(); theme.onNodeSelect(node.id) }
  }
  const label = node.alias || node.name
  return <Link to={`/node/${node.id}`} onClick={select} title={label} className="truncate font-semibold underline-offset-4 hover:underline">{children ?? label}</Link>
}

function NodeStatus({ node }: { node: ThemeNode }) {
  const { t } = useLocale()
  return <Badge variant={node.status === "online" ? "default" : "secondary"}>{t(node.status === "online" ? "node.online" : "node.offline")}</Badge>
}

function NodeSummaryCard({ node, devices }: { node: ThemeNode; devices?: AcceleratorDevicePublic[] }) {
  const { t } = useLocale()
  const definition = usePublicTheme()!.definition
  return <Card className="overflow-hidden"><CardHeader className="flex flex-row items-center justify-between gap-3">
    <CardTitle className="min-w-0 text-base"><NodeLink node={node} /></CardTitle><NodeStatus node={node} />
  </CardHeader><CardContent className="space-y-4">
    <TelemetryStatus node={node} /><AcceleratorSummary devices={devices} />
    {themeFlag(definition, "show_host") && node.host && <p className="truncate font-mono text-xs text-muted-foreground" title={node.host}>{node.host}</p>}
    {themeResources(definition).map((resource) => <ResourceLine key={resource} label={t(resourceSpec[resource].label)} value={node.metrics[resource]} />)}
    {themeFlag(definition, "show_network") && <div className="flex justify-between border-t pt-3 font-mono text-xs text-muted-foreground"><span>↓ {formatMetric(node.metrics.net_recv, "bytes-per-second")}</span><span>↑ {formatMetric(node.metrics.net_sent, "bytes-per-second")}</span></div>}
  </CardContent></Card>
}

function ResourceLine({ label, value }: { label: string; value: number | undefined }) {
  return <div className="space-y-1.5"><div className="flex justify-between text-xs"><span className="text-muted-foreground">{label}</span><span className="font-mono tabular-nums">{formatMetric(value, "percent")}</span></div>
    {value !== undefined && <Progress value={Math.max(0, Math.min(100, value))} aria-label={label} />}</div>
}

function NodeTile({ node, devices }: { node: ThemeNode; devices?: AcceleratorDevicePublic[] }) {
  const { t } = useLocale()
  const definition = usePublicTheme()!.definition
  const spacing = definition.density === "compact" ? "p-4 sm:p-5" : "p-6 sm:p-8"
  // 磁贴用 CPU 作为主指标，因此其余选中的资源进入下方网格。
  const secondary = themeResources(definition).filter((resource) => resource !== "cpu")
  return <Card className="gap-0 overflow-hidden py-0"><CardHeader className={`bg-primary/5 ${spacing}`}>
    <div className="mb-4 flex items-center justify-between gap-3"><CardTitle className="min-w-0 text-xl"><NodeLink node={node} /></CardTitle><NodeStatus node={node} /></div>
    <div className="flex items-end justify-between gap-4"><div><p className="mb-1 text-xs text-muted-foreground">{t("node.cpu")}</p><p className="font-mono text-4xl font-semibold tracking-tight tabular-nums">{formatMetric(node.metrics.cpu, "percent")}</p></div><CpuIcon className="size-8 text-primary" /></div>
  </CardHeader><CardContent className={`space-y-5 ${spacing}`}>
    <TelemetryStatus node={node} /><AcceleratorSummary devices={devices} />
    {secondary.length > 0 && <div className="grid grid-cols-2 gap-6">{secondary.map((resource) => {
      const Icon = resourceSpec[resource].icon
      return <div key={resource} className="space-y-2"><Icon className="size-4 text-muted-foreground" /><ResourceLine label={t(resourceSpec[resource].label)} value={node.metrics[resource]} /></div>
    })}</div>}
    {themeFlag(definition, "show_remark") && node.public_remark && <p className="line-clamp-3 text-sm text-muted-foreground">{node.public_remark}</p>}
    {themeFlag(definition, "show_tags") && <div className="flex flex-wrap gap-2">{node.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}</div>}
    {themeFlag(definition, "show_host") && node.host && <p className="truncate font-mono text-xs text-muted-foreground" title={node.host}>{node.host}</p>}
  </CardContent></Card>
}

function NodeOperationsTable({ nodes, accelerators }: { nodes: ThemeNode[]; accelerators?: GPUDevices }) {
  const { t } = useLocale()
  const definition = usePublicTheme()!.definition
  const compact = definition.density === "compact"
  const resources = themeResources(definition)
  const network = themeFlag(definition, "show_network")
  // 前两个资源列在小屏可见，其余从 md 起显示，保持默认主题在手机上的既有密度。
  const resourceCell = (index: number, value: string) => <TableCell key={resources[index]} className={cn("font-mono tabular-nums", index > 1 && "hidden md:table-cell")}>{value}</TableCell>
  return <Card className="overflow-hidden py-0"><Table><TableHeader><TableRow>
    <TableHead>{t("app.nodes")}</TableHead><TableHead>{t("node.status")}</TableHead>
    {resources.map((resource, index) => <TableHead key={resource} className={index > 1 ? "hidden md:table-cell" : undefined}>{t(resourceSpec[resource].label)}</TableHead>)}
    {network && <TableHead className="hidden lg:table-cell">{t("node.network")}</TableHead>}
    <TableHead className="hidden xl:table-cell">{t("node.uptime")}</TableHead>
  </TableRow></TableHeader><TableBody>{nodes.map((node) => <TableRow key={node.id} className={compact ? "[&>td]:py-2" : "[&>td]:py-4"}>
    <TableCell className="max-w-48"><NodeLink node={node} /><AcceleratorSummary devices={accelerators?.[node.id]} />{themeFlag(definition, "show_host") && node.host && <p className="truncate font-mono text-xs text-muted-foreground" title={node.host}>{node.host}</p>}<div className="mt-1"><TelemetryStatus node={node} className="max-w-36 text-[10px] [&>span]:hidden md:[&>span]:inline" /></div></TableCell><TableCell><NodeStatus node={node} /></TableCell>
    {resources.map((resource, index) => resourceCell(index, formatMetric(node.metrics[resource], "percent")))}
    {network && <TableCell className="hidden font-mono text-xs lg:table-cell">↓ {formatMetric(node.metrics.net_recv, "bytes-per-second")}<br />↑ {formatMetric(node.metrics.net_sent, "bytes-per-second")}</TableCell>}
    <TableCell className="hidden font-mono text-xs xl:table-cell">{node.metrics.uptime === undefined ? "--" : formatDuration(node.metrics.uptime)}</TableCell>
  </TableRow>)}</TableBody></Table></Card>
}

function formatMetric(value: number | undefined, format: Parameters<typeof formatPresentMetric>[1]): string {
  return value === undefined || !Number.isFinite(value) ? "--" : formatPresentMetric(value, format)
}
