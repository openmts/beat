import { Link } from "react-router"
import { Badge } from "@/components/ui/badge"
import { NodeCard } from "@/components/node-card"
import { PublicNodeMetadata } from "@/components/public-node-metadata"
import { TelemetryStatus } from "@/components/telemetry-status"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ThemedNodeCollection } from "@/components/themed-node-collection"
import { usePublicTheme } from "@/lib/theme-presentation"
import { useLocale } from "@/context/locale"
import { useFleetText } from "@/pages/fleet-text"
import { fleetName } from "@/lib/fleet-selectors"
import { formatBytes, formatMetricValue } from "@/lib/metric-format"
import type { Group, Node } from "@/types"
export function FleetResults({ nodes, allNodes, groups, layout, onInteraction }: { nodes: Node[]; allNodes: Node[]; groups: Group[]; layout: "cards" | "table"; onInteraction: (paused: boolean) => void }) {
 const t = useFleetText(), theme = usePublicTheme(), represented = [...new Set(nodes.map(node => node.group_id))]
 if (!nodes.length) return <p className="rounded-xl border border-dashed p-12 text-center text-muted-foreground">{t("empty")}</p>
 return <div id="fleet-results" tabIndex={-1} className="space-y-8 px-3 sm:px-5 lg:px-6" onPointerOver={() => onInteraction(true)} onPointerLeave={() => onInteraction(false)} onFocusCapture={() => onInteraction(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget as globalThis.Node | null)) onInteraction(false) }}>
  {represented.map(id => { const matching = nodes.filter(node => node.group_id === id), all = allNodes.filter(node => node.group_id === id), group = groups.find(group => group.id === id); return <section key={id} aria-labelledby={`fleet-group-${id}`} className="space-y-3">
   <div className="sticky top-12 z-10 flex flex-wrap items-center gap-3 border-b bg-background/95 py-3 backdrop-blur"><h2 id={`fleet-group-${id}`} className="text-base font-semibold">{group?.name ?? t("other")}</h2><Badge variant="outline">{matching.length}/{all.length}</Badge><span className="text-xs text-muted-foreground">{t("online")}: {matching.filter(node => node.status === "online").length}</span></div>
   {theme ? <ThemedNodeCollection accelerators={Object.fromEntries(matching.map(node => [node.id, node.accelerators ?? []]))} selectedGroup={id} fleetLayout={layout} hideGroupHeading /> : layout === "table" ? <FleetTable nodes={matching} /> : <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{matching.map(node => <NodeCard key={node.id} node={node} />)}</div>}
  </section> })}
 </div>
}
const percent = (value: number | undefined, locale: string) => Number.isFinite(value) ? formatMetricValue(value!, "percent", locale) : "—"
const capacity = (used: number | undefined, total: number | undefined, locale: string) => Number.isFinite(used) && Number.isFinite(total) && total! > 0 ? `${formatBytes(used!, locale)} / ${formatBytes(total!, locale)}` : "—"
export function FleetTable({ nodes }: { nodes: Node[] }) {
 const { locale } = useLocale(), t = useFleetText()
 return <div role="region" aria-label={t("table")} tabIndex={0} className="overflow-x-auto rounded-md border"><Table><TableHeader><TableRow>{["name", "status", "cpu", "memory", "disk", "upload", "download", "expiry"].map(key => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{nodes.map(node => {
  const metric = node.metrics ?? {}, name = fleetName(node), platform = [node.platform || node.os, node.arch].filter(Boolean).join(" / ")
  return <TableRow key={node.id}><TableCell className="max-w-64"><Link to={`/node/${encodeURIComponent(node.id)}`} title={name} className="font-semibold underline-offset-4 hover:underline">{name}</Link><p className="truncate text-xs text-muted-foreground" title={platform}>{platform}</p>{node.host && <p className="max-w-48 truncate font-mono text-xs" title={node.host}>{node.host}</p>}<div className="mt-1 flex flex-wrap gap-1">{node.tags.map(tag => <Badge key={tag} variant="outline">{tag}</Badge>)}</div></TableCell>
   <TableCell><Badge variant={node.status === "online" ? "default" : "secondary"}>{t(node.status === "online" ? "online" : "offlineStatus")}</Badge><TelemetryStatus node={node} className="mt-1" /></TableCell>
   <TableCell className="font-mono text-xs">{percent(metric.cpu, locale)}<p>{Number.isFinite(metric.cpu_used) && Number.isFinite(metric.cpu_total) ? `${metric.cpu_used}/${metric.cpu_total}` : "—"}</p></TableCell><TableCell className="font-mono text-xs">{percent(metric.memory, locale)}<p>{capacity(metric.memory_used, metric.memory_total, locale)}</p></TableCell><TableCell className="font-mono text-xs">{percent(metric.disk, locale)}<p>{capacity(metric.disk_used, metric.disk_total, locale)}</p></TableCell>
   <TableCell className="font-mono text-xs">{Number.isFinite(metric.net_sent) ? formatMetricValue(metric.net_sent, "bytes-per-second", locale) : "—"}</TableCell><TableCell className="font-mono text-xs">{Number.isFinite(metric.net_recv) ? formatMetricValue(metric.net_recv, "bytes-per-second", locale) : "—"}</TableCell><TableCell><PublicNodeMetadata node={node} /></TableCell>
  </TableRow>
 })}</TableBody></Table></div>
}
