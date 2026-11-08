import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowDownUpIcon,
  ArrowUpIcon,
  GaugeIcon,
  ServerIcon,
  type LucideIcon,
} from "lucide-react"

import { CountryBadge } from "@/components/country-badge"
import { useLocale } from "@/context/locale"
import { fleetName, fleetTotals } from "@/lib/fleet-selectors"
import { formatInteger, formatPercent } from "@/lib/intl-format"
import { formatBytes, formatMetricValue } from "@/lib/metric-format"
import { themeSurfaceClass, themeSummary, usePublicTheme } from "@/lib/theme-presentation"
import { cn } from "@/lib/utils"
import { useFleetText } from "@/pages/fleet-text"
import type { Group, Node } from "@/types"
import type { ThemeSummaryLayout, ThemeSummaryMetric } from "@/types/theme-definition"
import type { ReactNode } from "react"

interface FleetOverviewProps {
  nodes: Node[]
  unavailable: boolean
}

interface FleetGroupTabsProps {
  nodes: Node[]
  groups: Group[]
  value: string[]
  change: (groups: string[]) => void
}

export function FleetGroupTabs({ nodes, groups, value, change }: FleetGroupTabsProps) {
  const translate = useFleetText()
  return (
    <nav aria-label={translate("groups")} className="scrollbar-none flex min-w-0 items-center gap-1 overflow-x-auto">
      <GroupTab label={translate("all")} count={nodes.length} active={value.length === 0} onClick={() => change([])} />
      {groups.map((group) => <GroupTab key={group.id} label={group.name} count={nodes.filter((node) => node.group_id === group.id).length} active={value.length === 1 && value[0] === group.id} onClick={() => change([group.id])} />)}
    </nav>
  )
}

function GroupTab({ label, count, active, onClick }: { label: string; count: number; active: boolean; onClick: () => void }) {
  return (
    <button type="button" aria-pressed={active} onClick={onClick} className={`inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-sm font-medium whitespace-nowrap transition-colors ${active ? "bg-secondary text-secondary-foreground" : "text-foreground hover:bg-accent hover:text-accent-foreground"}`}>
      <span>{label}</span><span className="text-xs tabular-nums text-muted-foreground">{count}</span>
    </button>
  )
}

export function FleetOverview({ nodes, unavailable }: FleetOverviewProps) {
  const { locale } = useLocale()
  const translate = useFleetText()
  const definition = usePublicTheme()?.definition
  const summary = definition ? themeSummary(definition) : { layout: "cards" as const, metrics: ["availability", "busiest", "traffic", "throughput"] as ThemeSummaryMetric[] }
  const items = summaryItems(nodes, unavailable, locale, translate, summary.metrics)
  const surface = definition ? themeSurfaceClass(definition) : "border border-border/80 bg-card shadow-sm ring-0"
  const columns = Math.max(1, items.length)

  return <section aria-label={translate("overview")} data-theme-summary={summary.layout} className={summarySectionClass(summary.layout, columns, surface)}>
    {items.map((item, index) => <SummaryItemView key={item.metric} item={item} layout={summary.layout} index={index}
      className={itemClass(summary.layout, index, surface)} />)}
  </section>
}

interface SummaryItem {
  metric: ThemeSummaryMetric
  icon: LucideIcon
  label: string
  primary: ReactNode
  secondary?: ReactNode
  flows?: { download: string; upload: string }
}

function summaryItems(nodes: Node[], unavailable: boolean, locale: string, translate: (key: string) => string, metrics: ThemeSummaryMetric[]): SummaryItem[] {
  const totals = fleetTotals(nodes)
  const offline = Math.max(0, totals.total - totals.online)
  const busiest = busiestNode(nodes)
  const totalTraffic = trafficTotals(nodes, "net_sent_total", "net_recv_total", unavailable, locale)
  const realtime = trafficTotals(nodes, "net_sent", "net_recv", unavailable, locale, true)
  const count = (value: number) => unavailable ? "—" : formatInteger(locale, value)
  return metrics.map((metric) => {
    if (metric === "availability") return { metric, icon: ServerIcon, label: translate("total"),
      primary: <>{count(totals.online)} <span className="font-normal text-muted-foreground">/ {count(totals.total)}</span></>,
      secondary: `${count(offline)} ${translate("offlineStatus")}` }
    if (metric === "busiest") return { metric, icon: ActivityIcon, label: translate("busiest"),
      primary: !unavailable && busiest?.metrics?.cpu !== undefined ? formatPercent(locale, busiest.metrics.cpu, 1) : "—",
      secondary: busiest ? <span className="inline-flex min-w-0 items-center gap-1"><CountryBadge code={busiest.location?.country_code} /><span className="truncate">{fleetName(busiest)}</span></span> : translate("noTelemetry") }
    if (metric === "traffic") return { metric, icon: ArrowDownUpIcon, label: translate("totalTraffic"), primary: null, flows: totalTraffic }
    if (metric === "throughput") return { metric, icon: GaugeIcon, label: translate("realtimeNetwork"), primary: null, flows: realtime }
    return { metric, icon: ServerIcon, label: translate("groups"), primary: count(totals.groups), secondary: `${count(totals.countries)} ${translate("countries")}` }
  })
}

function summarySectionClass(layout: ThemeSummaryLayout, count: number, surface: string): string {
  if (layout === "strip") return cn("grid overflow-hidden rounded-xl", stripColumns(count), surface)
  if (layout === "focus") return "grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-[minmax(0,1.6fr)_repeat(3,minmax(0,1fr))]"
  if (layout === "minimal") return "flex flex-wrap items-stretch gap-x-6 gap-y-3 border-y py-3"
  return cn("grid grid-cols-2 gap-3", cardColumns(count))
}

function cardColumns(count: number): string {
  if (count <= 1) return "grid-cols-1"
  if (count === 2) return "lg:grid-cols-2"
  if (count === 3) return "lg:grid-cols-3"
  if (count === 4) return "lg:grid-cols-4"
  return "lg:grid-cols-5"
}

function stripColumns(count: number): string {
  if (count <= 1) return "grid-cols-1"
  if (count === 2) return "grid-cols-1 sm:grid-cols-2"
  if (count === 3) return "grid-cols-1 sm:grid-cols-3"
  if (count === 4) return "grid-cols-1 sm:grid-cols-2 xl:grid-cols-4"
  return "grid-cols-1 sm:grid-cols-2 xl:grid-cols-5"
}

function itemClass(layout: ThemeSummaryLayout, index: number, surface: string): string {
  if (layout === "strip") return "border-b border-border p-3 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0 xl:p-4"
  if (layout === "minimal") return "min-w-0 border-l pl-3 first:border-l-0 first:pl-0"
  if (layout === "focus" && index === 0) return cn("flex min-h-32 flex-col rounded-xl p-4 text-card-foreground lg:row-span-2 lg:min-h-40 lg:p-5", surface)
  return cn("flex min-h-24 flex-col rounded-xl p-3 text-card-foreground lg:min-h-28", surface)
}

function SummaryItemView({ item, layout, index, className }: { item: SummaryItem; layout: ThemeSummaryLayout; index: number; className: string }) {
  const translate = useFleetText()
  const Icon = item.icon
  const focused = layout === "focus" && index === 0
  return <div data-theme-summary-metric={item.metric} className={className}>
    <div className="flex items-center gap-1.5 text-xs text-muted-foreground"><Icon className="size-3.5" aria-hidden />{item.label}</div>
    {item.flows ? <div className={cn("mt-1 grid gap-x-3 gap-y-1", layout === "minimal" ? "grid-cols-2" : "grid-cols-1 sm:grid-cols-2")}>
      <FlowValue icon={ArrowDownIcon} label={translate("download")} value={item.flows.download} />
      <FlowValue icon={ArrowUpIcon} label={translate("upload")} value={item.flows.upload} />
    </div> : <p className={cn("mt-1 min-w-0 truncate font-semibold tabular-nums", focused ? "text-3xl lg:text-4xl" : layout === "minimal" ? "text-lg" : "text-xl")} title={typeof item.primary === "string" ? item.primary : undefined}>{item.primary}</p>}
    {item.secondary && <p className={cn("truncate text-xs text-muted-foreground", layout === "minimal" ? "mt-0.5" : "mt-auto pt-1")}>{item.secondary}</p>}
  </div>
}

function FlowValue({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: string }) {
  return <p className="flex min-w-0 items-center gap-1 text-sm font-semibold tabular-nums" title={`${label}: ${value}`}><Icon className="size-3 shrink-0 text-muted-foreground" aria-hidden /><span className="sr-only">{label}</span><span className="truncate">{value}</span></p>
}

function busiestNode(nodes: Node[]): Node | undefined {
  return nodes.filter((node) => node.telemetry_state === "fresh" && Number.isFinite(node.metrics?.cpu)).sort((first, second) => (second.metrics?.cpu ?? 0) - (first.metrics?.cpu ?? 0))[0]
}

function trafficTotals(nodes: Node[], sentKey: string, receivedKey: string, unavailable: boolean, locale: string, perSecond = false): { download: string; upload: string } {
  if (unavailable) return { download: "—", upload: "—" }
  const sum = (key: string) => nodes.reduce((total, node) => { const value = node.metrics?.[key]; return total + (Number.isFinite(value) ? value ?? 0 : 0) }, 0)
  const format = (value: number) => perSecond ? formatMetricValue(value, "bytes-per-second", locale) : formatBytes(value, locale)
  return { download: format(sum(receivedKey)), upload: format(sum(sentKey)) }
}
