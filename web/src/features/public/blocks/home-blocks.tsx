import { Link } from "@tanstack/react-router";
import { Activity, CircleDashed, Gauge, Layers3, Network, RefreshCw, Search, Server, ShieldAlert, WifiOff, X } from "lucide-react";
import type { PublicThemeBlock } from "@/api/types";
import { NodeCard } from "@/components/data/node-card";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Segmented } from "@/components/ui/segmented";
import type { PublicHomeRuntime } from "@/features/public/renderer/public-runtime";
import { publicGridClass } from "@/features/public/renderer/public-runtime";
import { formatBytes, formatPercent, formatRate, formatRelative, formatUptime, statusLabel } from "@/lib/format";
import { groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";

type HomeBlockProps = { block: PublicThemeBlock; runtime: PublicHomeRuntime };

function homeRuntime(runtime: HomeBlockProps["runtime"]): PublicHomeRuntime {
  return runtime as PublicHomeRuntime;
}

function propString(block: PublicThemeBlock, key: string, fallback = "") {
  const value = block.props[key];
  return typeof value === "string" ? value : fallback;
}

export function HeroBlock({ block }: HomeBlockProps) {
  return (
    <section className="relative overflow-hidden rounded-2xl border bg-card shadow-panel">
      <div className="pointer-events-none absolute inset-y-0 right-0 hidden w-[44%] bg-[radial-gradient(circle_at_75%_35%,hsl(var(--primary)/0.14),transparent_58%),linear-gradient(135deg,transparent_20%,hsl(var(--public-accent,var(--primary))/0.05))] lg:block" />
      <div className="pointer-events-none absolute right-8 top-6 hidden grid-cols-6 gap-2 opacity-50 lg:grid">
        {Array.from({ length: 24 }).map((_, index) => <span key={index} className="h-1 w-1 rounded-full bg-primary/30" />)}
      </div>
      <div className="relative max-w-2xl">
        <Badge variant="outline" className="border-primary/15 bg-primary/[0.04] text-primary">{propString(block, "eyebrow", "Node observatory")}</Badge>
        <h1 className="mt-4 text-3xl font-semibold tracking-tight sm:text-4xl">{propString(block, "title", "网络节点运行概览")}</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground sm:text-[15px]">{propString(block, "description")}</p>
      </div>
    </section>
  );
}

const SUMMARY_META = {
  all: { label: "节点总数", icon: Server, tone: "primary" as const },
  online: { label: "在线", icon: Activity, tone: "success" as const },
  warning: { label: "高负载", icon: ShieldAlert, tone: "warning" as const },
  offline: { label: "离线", icon: WifiOff, tone: "danger" as const },
};

export function HomeSummaryBlock({ block, runtime }: HomeBlockProps) {
  const data = homeRuntime(runtime);
  if (propString(block, "variant") === "minimal") return <MinimalSummaryGrid runtime={data} />;
  const requested = Array.isArray(block.props.items) ? block.props.items.filter((item): item is string => typeof item === "string") : ["all", "online", "warning", "offline"];
  const items = requested.filter((item): item is keyof typeof SUMMARY_META => item in SUMMARY_META);
  return (
    <div className={publicGridClass(Math.max(1, Math.min(4, block.layout.columns || items.length)))}>
      {items.map((key) => {
        const meta = SUMMARY_META[key];
        return <SummaryTile key={key} label={meta.label} value={data.counts[key] ?? 0} icon={meta.icon} tone={meta.tone} />;
      })}
    </div>
  );
}

export function GroupFilterBlock({ block, runtime }: HomeBlockProps) {
  const data = homeRuntime(runtime);
  if (propString(block, "variant") === "minimal") return <MinimalGroupFilter runtime={data} />;
  const groups = data.groups;
  return (
    <section className="rounded-xl border bg-card shadow-panel">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2"><Layers3 className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">{propString(block, "title", "按分组查看")}</h2></div>
        <p className="text-[10px] text-muted-foreground">{propString(block, "description", "选择分组，只看旗下节点")}</p>
      </div>
      <div className="flex gap-2 overflow-x-auto pb-1 scrollbar-thin" role="group" aria-label="节点分组筛选">
        <button type="button" aria-pressed={data.group === "all"} onClick={() => data.setGroup("all")} className={cn("inline-flex shrink-0 items-center gap-2 rounded-lg border px-3 py-2 text-[11px] font-medium transition-colors", data.group === "all" ? "border-primary/25 bg-primary/[0.07] text-primary" : "bg-background/60 text-muted-foreground hover:text-foreground")}>
          <span className="h-2 w-2 rounded-full bg-primary" />全部分组<Badge variant="outline" className="px-1.5 py-0 text-[9px]">{data.counts.all ?? 0}</Badge>
        </button>
        {groups?.items.map((group) => {
          const palette = groupColor(group.color);
          return (
            <button key={group.id} type="button" aria-pressed={data.group === group.id} onClick={() => data.setGroup(group.id)} className={cn("inline-flex shrink-0 items-center gap-2 rounded-lg border px-3 py-2 text-[11px] font-medium transition-colors", data.group === group.id ? palette.chip : "bg-background/60 text-muted-foreground hover:text-foreground")}>
              <span className={cn("h-2 w-2 rounded-full", palette.dot)} />{group.name}<span className={cn("rounded border px-1.5 py-0 text-[9px]", data.group === group.id ? "border-current/15 bg-white/35 dark:bg-black/10" : "bg-muted/50")}>{group.nodeCount}</span>
            </button>
          );
        })}
        {(groups?.ungroupedCount ?? 0) > 0 ? (
          <button type="button" aria-pressed={data.group === "ungrouped"} onClick={() => data.setGroup("ungrouped")} className={cn("inline-flex shrink-0 items-center gap-2 rounded-lg border border-dashed px-3 py-2 text-[11px] font-medium transition-colors", data.group === "ungrouped" ? "border-primary/25 bg-primary/[0.07] text-primary" : "bg-background/60 text-muted-foreground hover:text-foreground")}>
            <CircleDashed className="h-3 w-3" />未分组<span className="rounded border bg-muted/50 px-1.5 py-0 text-[9px]">{groups?.ungroupedCount}</span>
          </button>
        ) : null}
      </div>
    </section>
  );
}

export function NodeCollectionBlock({ block, runtime }: HomeBlockProps) {
  const data = homeRuntime(runtime);
  if (propString(block, "variant") === "minimal") return <MinimalNodeCollection block={block} runtime={data} />;
  const view = propString(block, "view", "card");
  const title = propString(block, "title", "节点列表");
  const description = propString(block, "description", "点击卡片查看完整指标监控");
  return (
    <section className="space-y-4">
      <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-panel lg:flex-row lg:items-center lg:justify-between">
        <div className="flex items-center gap-2.5">
          <div><h2 className="text-sm font-semibold">{data.group === "all" ? title : "分组节点"}</h2><p className="mt-0.5 text-[11px] text-muted-foreground">{data.group === "all" ? description : "仅展示当前分组下的节点"}</p></div>
          <Badge variant="outline" className="ml-1">{data.nodes.length}</Badge>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="relative min-w-0 sm:w-64">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input value={data.query} onChange={(event) => data.setQuery(event.target.value)} placeholder="搜索主机名、IP 或系统" className="pl-9 pr-9" />
            {data.query ? <button type="button" aria-label="清空搜索" onClick={() => data.setQuery("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"><X className="h-3.5 w-3.5" /></button> : null}
          </div>
          <Segmented value={data.status} onChange={data.setStatus} options={[{ value: "all", label: "全部" }, { value: "online", label: "在线" }, { value: "warning", label: "高负载" }, { value: "offline", label: "离线" }]} />
          <Button variant="ghost" size="icon" onClick={data.refresh} disabled={data.nodesLoading} aria-label="刷新节点"><RefreshCw className={cn("h-4 w-4", data.nodesLoading && "animate-spin")} /></Button>
        </div>
      </div>

      {data.nodesLoading ? <LoadingState label="正在读取节点状态…" /> : data.error ? <ErrorState message={data.error} onRetry={data.refresh} /> : data.nodes.length === 0 ? (
        <EmptyState title="没有匹配的节点" description="调整搜索条件或状态筛选后重试。" />
      ) : view === "table" ? (
        <div className={publicGridClass(1)}>{data.nodes.map((node) => <CompactNodeRow key={node.id} node={node} />)}</div>
      ) : (
        <div className={publicGridClass(block.layout.columns || 3)}>{data.nodes.map((node) => <NodeCard key={node.id} node={node} />)}</div>
      )}
    </section>
  );
}

function MinimalSummaryGrid({ runtime }: { runtime: PublicHomeRuntime }) {
  const activeNodes = runtime.nodes.filter((node) => node.status !== "offline");
  const all = runtime.counts.all ?? runtime.nodes.length;
  const online = runtime.counts.online ?? activeNodes.filter((node) => node.status === "online").length;
  const offline = runtime.counts.offline ?? 0;
  const busiest = activeNodes.reduce<PublicItem | null>((current, node) => !current || node.metrics.cpuUsage > current.metrics.cpuUsage ? node : current, null);
  const averageCpu = activeNodes.length ? activeNodes.reduce((sum, node) => sum + node.metrics.cpuUsage, 0) / activeNodes.length : 0;
  const averageMemory = activeNodes.length ? activeNodes.reduce((sum, node) => sum + node.metrics.memoryUsage, 0) / activeNodes.length : 0;
  const networkRx = activeNodes.reduce((sum, node) => sum + node.metrics.networkRxBps, 0);
  const networkTx = activeNodes.reduce((sum, node) => sum + node.metrics.networkTxBps, 0);
  return (
    <div className="minimal-summary-grid grid grid-cols-2 gap-2 xl:grid-cols-4">
      <MinimalStat icon={Server} label="节点" value={`${online}/${all}`} hint={`${offline} 个离线 · ${runtime.counts.warning ?? 0} 个高负载`} />
      <MinimalStat icon={Activity} label="最忙节点" value={busiest ? formatPercent(busiest.metrics.cpuUsage) : "—"} hint={busiest?.hostname ?? "暂无在线节点"} />
      <MinimalStat icon={Gauge} label="平均资源" value={formatPercent(averageCpu)} hint={`内存 ${formatPercent(averageMemory)} · 在线节点均值`} />
      <MinimalStat icon={Network} label="实时网速" value={`↓ ${formatRate(networkRx)}`} hint={`↑ ${formatRate(networkTx)} · 全节点合计`} />
    </div>
  );
}

function MinimalStat({ icon: Icon, label, value, hint }: { icon: typeof Server; label: string; value: string; hint: string }) {
  return (
    <div className="min-w-0 rounded-md border bg-card p-3">
      <p className="flex items-center gap-1.5 text-[10px] text-muted-foreground"><Icon className="h-3 w-3" />{label}</p>
      <p className="mt-2 truncate text-lg font-semibold leading-none tracking-tight tabular-nums">{value}</p>
      <p className="mt-2 truncate text-[10px] text-muted-foreground">{hint}</p>
    </div>
  );
}

function MinimalGroupFilter({ runtime }: { runtime: PublicHomeRuntime }) {
  const groups = runtime.groups;
  const base = "inline-flex h-8 shrink-0 items-center gap-2 rounded-md px-3 text-[11px] font-medium transition-colors";
  const selected = "bg-primary text-primary-foreground";
  const idle = "text-muted-foreground hover:bg-muted hover:text-foreground";
  return (
    <div className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 scrollbar-thin" role="group" aria-label="节点分组筛选">
      <button type="button" aria-pressed={runtime.group === "all"} onClick={() => runtime.setGroup("all")} className={cn(base, runtime.group === "all" ? selected : idle)}>
        全部<span className="tabular-nums opacity-65">{runtime.counts.all ?? 0}</span>
      </button>
      {groups?.items.map((group) => (
        <button key={group.id} type="button" aria-pressed={runtime.group === group.id} onClick={() => runtime.setGroup(group.id)} className={cn(base, runtime.group === group.id ? selected : idle)}>
          {group.name}<span className="tabular-nums opacity-65">{group.nodeCount}</span>
        </button>
      ))}
      {(groups?.ungroupedCount ?? 0) > 0 ? (
        <button type="button" aria-pressed={runtime.group === "ungrouped"} onClick={() => runtime.setGroup("ungrouped")} className={cn(base, runtime.group === "ungrouped" ? selected : idle)}>
          未分组<span className="tabular-nums opacity-65">{groups?.ungroupedCount}</span>
        </button>
      ) : null}
    </div>
  );
}

function MinimalNodeCollection({ block, runtime }: HomeBlockProps) {
  const data = homeRuntime(runtime);
  const title = propString(block, "title", "节点");
  const description = propString(block, "description", "点击节点查看指标详情");
  return (
    <section className="space-y-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-baseline gap-2">
          <h2 className="text-sm font-semibold">{data.group === "all" ? title : "分组节点"}</h2>
          <span className="text-[11px] tabular-nums text-muted-foreground">{data.nodes.length}</span>
          <span className="hidden truncate text-[10px] text-muted-foreground md:inline">{data.group === "all" ? description : "仅展示当前分组下的节点"}</span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-0 flex-1 sm:w-52 sm:flex-none">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input value={data.query} onChange={(event) => data.setQuery(event.target.value)} placeholder="搜索主机、IP 或系统" className="h-8 rounded-md pl-8 pr-8 text-xs" />
            {data.query ? <button type="button" aria-label="清空搜索" onClick={() => data.setQuery("")} className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"><X className="h-3.5 w-3.5" /></button> : null}
          </div>
          <Segmented value={data.status} onChange={data.setStatus} options={[{ value: "all", label: "全部" }, { value: "online", label: "在线" }, { value: "warning", label: "高负载" }, { value: "offline", label: "离线" }]} />
          <Button variant="ghost" size="icon" onClick={data.refresh} disabled={data.nodesLoading} aria-label="刷新节点" className="h-8 w-8"><RefreshCw className={cn("h-3.5 w-3.5", data.nodesLoading && "animate-spin")} /></Button>
        </div>
      </div>

      {data.nodesLoading ? <LoadingState label="正在读取节点状态…" /> : data.error ? <ErrorState message={data.error} onRetry={data.refresh} /> : data.nodes.length === 0 ? (
        <EmptyState title="没有匹配的节点" description="调整搜索条件或状态筛选后重试。" />
      ) : (
        <div className={publicGridClass(block.layout.columns || 4)}>{data.nodes.map((node) => <MinimalNodeCard key={node.id} node={node} />)}</div>
      )}
    </section>
  );
}

function MinimalNodeCard({ node }: { node: PublicItem }) {
  const active = node.status !== "offline";
  const statusClass = node.status === "online" ? "bg-success" : node.status === "warning" ? "bg-warning" : "bg-destructive";
  return (
    <Link to="/nodes/$nodeId" params={{ nodeId: node.id }} className="group block min-w-0 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <article className={cn("h-full min-w-0 rounded-md border bg-card p-3 transition-colors group-hover:border-foreground/30", !active && "border-destructive/20 bg-destructive/[0.025]")}>
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", statusClass)} />
          <h3 className="truncate text-[13px] font-semibold tracking-tight">{node.hostname}</h3>
          <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{statusLabel(node.status)}</span>
        </div>
        <p className="mt-1 truncate text-[10px] text-muted-foreground">{node.groupName || "未分组"} · {node.os || "未知系统"} · {node.arch || "unknown"}</p>
        <p className="mt-1 truncate text-[10px] text-muted-foreground">{active ? `在线 ${formatUptime(node.metrics.uptimeSeconds)}` : `最后上报 ${formatRelative(node.lastSeen)}`}</p>

        <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2.5">
          <MinimalMetric label="CPU" value={active ? formatPercent(node.metrics.cpuUsage) : "—"} hint={`${node.cpuCores} 核 · ${active ? node.metrics.load1.toFixed(2) : "负载未知"}`} progress={active ? node.metrics.cpuUsage : undefined} />
          <MinimalMetric label="内存" value={active ? formatPercent(node.metrics.memoryUsage) : "—"} hint={active ? `${formatBytes(node.metrics.memoryUsedBytes)} / ${formatBytes(node.metrics.memoryTotalBytes || node.totalMemoryBytes)}` : formatBytes(node.metrics.memoryTotalBytes || node.totalMemoryBytes)} progress={active ? node.metrics.memoryUsage : undefined} />
          <MinimalMetric label="磁盘" value={active ? formatPercent(node.metrics.diskUsage) : "—"} hint={active ? `${formatBytes(node.metrics.diskUsedBytes)} / ${formatBytes(node.metrics.diskTotalBytes || node.totalDiskBytes)}` : formatBytes(node.metrics.diskTotalBytes || node.totalDiskBytes)} progress={active ? node.metrics.diskUsage : undefined} />
          <MinimalMetric label="网络" value={active ? formatRate(node.metrics.networkRxBps + node.metrics.networkTxBps) : "—"} hint={active ? `↓ ${formatRate(node.metrics.networkRxBps)} · ↑ ${formatRate(node.metrics.networkTxBps)}` : "当前状态不可用"} />
        </div>

        <div className="mt-3 flex items-center justify-between gap-2 border-t pt-2 text-[9px] text-muted-foreground">
          <span className="truncate">{Object.keys(node.labels).length ? `${Object.keys(node.labels).length} 个标签` : "无标签"}</span>
          <span className="shrink-0">采集 {node.collectorIntervalSeconds}s</span>
        </div>
      </article>
    </Link>
  );
}

function MinimalMetric({ label, value, hint, progress }: { label: string; value: string; hint: string; progress?: number }) {
  return (
    <div className="min-w-0">
      <div className="flex items-center justify-between gap-2 text-[10px]"><span className="text-muted-foreground">{label}</span><span className="font-medium tabular-nums">{value}</span></div>
      {progress !== undefined ? <div className="mt-1.5 h-px overflow-hidden bg-border"><div className={cn("h-full bg-foreground/65", progress >= 90 && "bg-warning")} style={{ width: `${Math.max(0, Math.min(100, progress))}%` }} /></div> : null}
      <p className="mt-1 truncate text-[9px] text-muted-foreground">{hint}</p>
    </div>
  );
}

function SummaryTile({ label, value, icon: Icon, tone }: { label: string; value: number; icon: typeof Server; tone: "primary" | "success" | "warning" | "danger" }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card p-4 shadow-panel">
      <div className={cn("grid h-9 w-9 shrink-0 place-items-center rounded-lg", {
        "bg-primary/10 text-primary": tone === "primary",
        "bg-success/10 text-success": tone === "success",
        "bg-warning/10 text-warning": tone === "warning",
        "bg-destructive/10 text-destructive": tone === "danger",
      })}><Icon className="h-4 w-4" /></div>
      <div><p className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{label}</p><p className="mt-0.5 text-xl font-semibold tabular-nums">{value}</p></div>
    </div>
  );
}

function CompactNodeRow({ node }: { node: PublicItem }) {
  return (
    <a href={`/nodes/${encodeURIComponent(node.id)}`} className="flex items-center justify-between gap-4 rounded-xl border bg-card px-4 py-3 transition-colors hover:border-primary/25">
      <div className="min-w-0"><p className="truncate text-sm font-semibold">{node.hostname}</p><p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">{node.address || "地址未上报"}</p></div>
      <div className="flex items-center gap-4 text-xs"><span className="text-muted-foreground">CPU {node.metrics.cpuUsage.toFixed(1)}%</span><span className="text-muted-foreground">内存 {node.metrics.memoryUsage.toFixed(1)}%</span><Badge variant="outline">{node.status === "online" ? "在线" : node.status === "warning" ? "高负载" : "离线"}</Badge></div>
    </a>
  );
}

type PublicItem = PublicHomeRuntime["nodes"][number];
