import { Link } from "@tanstack/react-router";
import { Activity, ArrowLeft, Boxes, Cpu, HardDrive, MemoryStick, RefreshCw, Server, Wifi } from "lucide-react";
import type { PublicThemeBlock } from "@/api/types";
import { TrendChart, type TrendSeries } from "@/components/charts/trend-chart";
import { MetricCard } from "@/components/data/metric-card";
import { LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Segmented } from "@/components/ui/segmented";
import type { PublicDetailRuntime } from "@/features/public/renderer/public-runtime";
import { publicGridClass } from "@/features/public/renderer/public-runtime";
import { NetworkProbeSection } from "@/features/nodes/network-probe-section";
import { formatBytes, formatPercent, formatRate, formatRelative, formatTime, formatUptime } from "@/lib/format";
import { groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";

type DetailBlockProps = { block: PublicThemeBlock; runtime: PublicDetailRuntime };

function detailRuntime(runtime: DetailBlockProps["runtime"]) {
  return runtime as PublicDetailRuntime;
}

export function NodeHeaderBlock({ runtime }: DetailBlockProps) {
  const data = detailRuntime(runtime);
  const node = data.node;
  const group = node.groupId ? groupColor(node.groupColor) : null;
  return (
    <div className="space-y-4">
      <Link to="/" className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"><ArrowLeft className="h-3.5 w-3.5" />返回节点广场</Link>
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-[0.16em] text-primary">Node detail</p>
          <h1 className="text-2xl font-semibold tracking-tight sm:text-[28px]">{node.hostname}</h1>
          <p className="mt-1.5 flex flex-wrap items-center gap-2 text-sm text-muted-foreground"><span>{node.address || "未上报地址"}</span><span>·</span><span>{group ? node.groupName : "未分组"}</span><span>·</span><span>最近上报 {formatRelative(node.lastSeen)}</span></p>
        </div>
        <div className="flex flex-wrap items-center gap-2"><StatusBadge status={node.status} className="h-7 px-3" /><Button variant="outline" size="sm" onClick={data.refresh}><RefreshCw className="h-3.5 w-3.5" />刷新</Button></div>
      </div>
      {node.status === "offline" ? <div className="rounded-xl border border-destructive/20 bg-destructive/[0.06] px-4 py-3 text-xs text-destructive">该节点已超过离线判定窗口未上报，最后通信时间为 {formatRelative(node.lastSeen)}。</div> : null}
      {node.status === "warning" ? <div className="rounded-xl border border-warning/25 bg-warning/[0.06] px-4 py-3 text-xs text-warning">节点仍在上报，但至少一项资源使用率超过 90%，建议检查系统负载。</div> : null}
    </div>
  );
}

export function SystemInfoBlock({ runtime }: DetailBlockProps) {
  const data = detailRuntime(runtime);
  const node = data.node;
  const group = node.groupId ? groupColor(node.groupColor) : null;
  const labels = Object.entries(node.labels);
  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b bg-gradient-to-r from-card to-primary/[0.025]">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><Server className="h-4 w-4 text-primary" />系统信息与节点标签</CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">主机环境、接入状态、分组与标签集中在节点详情顶部。</p>
          </div>
          <span className="inline-flex items-center gap-1.5 self-start rounded-md bg-muted/55 px-2 py-1 text-[10px] text-muted-foreground">
            <span className={cn("h-1.5 w-1.5 rounded-full", node.status === "online" ? "bg-success" : node.status === "warning" ? "bg-warning" : "bg-destructive")} />采集间隔 {node.collectorIntervalSeconds}s
          </span>
        </div>
      </CardHeader>
      <CardContent className="detail-info-shell p-0">
        <div className="detail-info-grid">
          <section className="p-5">
            <div className="flex items-center gap-2"><Cpu className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">系统与运行环境</h2></div>
            <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-x-6">
              <InfoRow label="操作系统" value={node.os} />
              <InfoRow label="内核版本" value={node.kernel} mono />
              <InfoRow label="CPU 型号" value={node.cpuModel} />
              <InfoRow label="CPU 核心" value={`${node.cpuCores} 线程`} />
              <InfoRow label="架构" value={node.arch} mono />
              <InfoRow label="运行时长" value={formatUptime(node.metrics.uptimeSeconds)} />
              <InfoRow label="内存总量" value={formatBytes(node.totalMemoryBytes)} />
              <InfoRow label="磁盘总量" value={formatBytes(node.totalDiskBytes)} />
            </div>
          </section>
          <section className="p-5">
            <div className="flex items-center gap-2"><Boxes className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">分组</h2></div>
            <div className="mt-3 flex items-center justify-between gap-3 rounded-lg border bg-background/55 p-3">
              <div className="min-w-0"><p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">所属分组</p><p className="mt-1 truncate text-sm font-semibold">{group ? node.groupName : "未分组"}</p></div>
              <span className={cn("inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2 py-1 text-[10px]", group?.chip ?? "border-dashed bg-muted/35 text-muted-foreground")}><span className={cn("h-1.5 w-1.5 rounded-full", group?.dot ?? "bg-muted-foreground/50")} />{group ? node.groupName : "待分组"}</span>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-2 text-[10px]">
              <InfoTile icon={Activity} label="首次接入" value={formatTime(node.firstSeen)} />
              <InfoTile icon={RefreshCw} label="最后上报" value={formatRelative(node.lastSeen)} />
            </div>
          </section>
          <section className="p-5">
            <div className="flex items-center justify-between gap-2"><h2 className="text-sm font-semibold">节点标签</h2><Badge variant="outline">{labels.length}</Badge></div>
            <div className="mt-3 flex min-h-16 flex-wrap content-start gap-1.5">
              {labels.length > 0 ? labels.map(([key, value]) => <span key={key} className="rounded-md border bg-background/70 px-2 py-1 font-mono text-[10px] text-muted-foreground">{key}={value}</span>) : <span className="text-xs text-muted-foreground">未配置节点标签</span>}
            </div>
          </section>
        </div>
      </CardContent>
    </Card>
  );
}

export function MetricSummaryBlock({ block, runtime }: DetailBlockProps) {
  const node = detailRuntime(runtime).node;
  return (
    <div className={publicGridClass(Math.max(1, Math.min(4, block.layout.columns || 4)))}>
      <MetricCard label="CPU 使用率" value={formatPercent(node.metrics.cpuUsage)} hint={`${node.cpuCores} 线程 · 负载 ${node.metrics.load1.toFixed(2)}`} icon={Cpu} tone={node.metrics.cpuUsage >= 85 ? "warning" : "success"} />
      <MetricCard label="内存使用率" value={formatPercent(node.metrics.memoryUsage)} hint={`${formatBytes(node.metrics.memoryUsedBytes)} / ${formatBytes(node.metrics.memoryTotalBytes)}`} icon={MemoryStick} tone={node.metrics.memoryUsage >= 85 ? "warning" : "violet"} />
      <MetricCard label="磁盘使用率" value={formatPercent(node.metrics.diskUsage)} hint={`${formatBytes(node.metrics.diskUsedBytes)} / ${formatBytes(node.metrics.diskTotalBytes)}`} icon={HardDrive} tone={node.metrics.diskUsage >= 85 ? "warning" : "primary"} />
      <MetricCard label="网络吞吐" value={formatRate(node.metrics.networkRxBps + node.metrics.networkTxBps)} hint={`下行 ${formatRate(node.metrics.networkRxBps)} · 上行 ${formatRate(node.metrics.networkTxBps)}`} icon={Wifi} tone="success" />
    </div>
  );
}

export function MetricTrendsBlock({ block, runtime }: DetailBlockProps) {
  const data = detailRuntime(runtime);
  const node = data.node;
  const points = data.metrics?.points ?? [];
  const timestamps = points.map((point) => point.timestamp);
  const chartKeys = Array.isArray(block.props.charts) ? block.props.charts.filter((item): item is string => typeof item === "string") : ["cpu", "memory", "disk", "network", "load"];
  const series: Record<string, TrendSeries[]> = {
    cpu: [
      { key: "cpu-total", label: "总使用率", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.cpuUsage) },
      { key: "cpu-user", label: "用户态", color: "hsl(var(--chart-memory))", values: points.map((point) => point.cpuUser) },
      { key: "cpu-system", label: "系统态", color: "hsl(var(--chart-disk))", values: points.map((point) => point.cpuSystem) },
    ],
    memory: [{ key: "memory", label: "内存使用率", color: "hsl(var(--chart-memory))", values: points.map((point) => point.memoryUsage) }],
    disk: [{ key: "disk", label: "磁盘使用率", color: "hsl(var(--chart-disk))", values: points.map((point) => point.diskUsage) }],
    network: [
      { key: "network-rx", label: "下行", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.networkRxBps) },
      { key: "network-tx", label: "上行", color: "hsl(var(--chart-network))", values: points.map((point) => point.networkTxBps) },
    ],
    load: [
      { key: "load-1", label: "1 分钟负载", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.load1) },
      { key: "load-5", label: "5 分钟负载", color: "hsl(var(--chart-memory))", values: points.map((point) => point.load5) },
      { key: "load-15", label: "15 分钟负载", color: "hsl(var(--chart-disk))", values: points.map((point) => point.load15) },
    ],
  };
  const networkYMax = niceChartMax(points.reduce((maximum, point) => Math.max(maximum, point.networkRxBps, point.networkTxBps), 0));
  const loadYMax = niceLinearMax(points.reduce((maximum, point) => Math.max(maximum, point.load1, point.load5, point.load15), 0));
  const columns = block.layout.columns || 2;
  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-panel sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold"><Activity className="h-4 w-4 text-primary" />资源监控视图</h2>
          <p className="mt-1 text-[11px] text-muted-foreground">来自 MTS 多值时序数据 · 采样步长 {data.metrics?.stepSeconds ? `${Math.round(data.metrics.stepSeconds / 60)} 分钟` : "—"}</p>
        </div>
        <Segmented value={data.range} onChange={data.setRange} options={[{ value: "1h", label: "1H" }, { value: "6h", label: "6H" }, { value: "24h", label: "24H" }, { value: "7d", label: "7D" }]} />
      </div>
      {data.metricsLoading ? <Card><CardContent className="pt-5"><LoadingState label="正在查询时序数据…" /></CardContent></Card> : data.metricsError ? <div className="rounded-xl border border-destructive/20 bg-destructive/[0.06] px-4 py-3 text-xs text-destructive">{data.metricsError}</div> : (
        <div className={publicGridClass(Math.max(1, Math.min(4, columns)))}>
          {chartKeys.includes("cpu") ? <MetricTrendCard title="CPU 使用率" description={`总使用率与用户态、系统态分解 · ${node.cpuCores} 线程`} icon={Cpu} series={series.cpu} timestamps={timestamps} /> : null}
          {chartKeys.includes("memory") ? <MetricTrendCard title="内存使用率" description={`${formatBytes(node.metrics.memoryUsedBytes)} / ${formatBytes(node.metrics.memoryTotalBytes)}`} icon={MemoryStick} series={series.memory} timestamps={timestamps} /> : null}
          {chartKeys.includes("disk") ? <MetricTrendCard title="磁盘使用率" description={`${formatBytes(node.metrics.diskUsedBytes)} / ${formatBytes(node.metrics.diskTotalBytes)}`} icon={HardDrive} series={series.disk} timestamps={timestamps} /> : null}
          {chartKeys.includes("network") ? <MetricTrendCard title="网络吞吐" description={`当前下行 ${formatRate(node.metrics.networkRxBps)} · 上行 ${formatRate(node.metrics.networkTxBps)}`} icon={Wifi} series={series.network} timestamps={timestamps} yMax={networkYMax} yTicks={[1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * networkYMax)} yTickFormatter={formatRate} valueFormatter={formatRate} /> : null}
          {chartKeys.includes("load") ? <MetricTrendCard className={columns === 2 ? "sm:col-span-2" : ""} title="系统负载" description={`1 / 5 / 15 分钟 Load Average · ${node.cpuCores} 线程`} icon={Boxes} series={series.load} timestamps={timestamps} yMax={loadYMax} yTicks={[1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * loadYMax)} yTickFormatter={(value) => value.toFixed(value < 10 ? 1 : 0)} valueFormatter={(value) => value.toFixed(2)} /> : null}
        </div>
      )}
    </div>
  );
}

export function NetworkProbesBlock({ runtime }: DetailBlockProps) {
  const data = detailRuntime(runtime);
  if (!data.probes || data.probes.tasks.length === 0) return null;
  return <NetworkProbeSection data={data.probes} />;
}

function MetricTrendCard({ title, description, icon: Icon, series, timestamps, yMax = 100, yTicks, yTickFormatter, valueFormatter = formatPercent, className }: {
  title: string;
  description: string;
  icon: typeof Cpu;
  series: TrendSeries[];
  timestamps: string[];
  yMax?: number;
  yTicks?: number[];
  yTickFormatter?: (value: number) => string;
  valueFormatter?: (value: number) => string;
  className?: string;
}) {
  const allValues = series.flatMap((item) => item.values).filter((value): value is number => value !== null && Number.isFinite(value));
  const peak = allValues.length > 0 ? Math.max(...allValues) : 0;
  return (
    <Card className={cn("overflow-hidden", className)}>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2"><Icon className="h-4 w-4 text-primary" />{title}</CardTitle>
            <p className="mt-1 truncate text-[11px] text-muted-foreground">{description}</p>
          </div>
          <div className="shrink-0 text-right"><p className="text-[9px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">区间峰值</p><p className="mt-1 text-sm font-semibold tabular-nums">{valueFormatter(peak)}</p></div>
        </div>
      </CardHeader>
      <CardContent className="pt-1">
        <TrendChart timestamps={timestamps} series={series} height={230} yMax={yMax} yTicks={yTicks} yTickFormatter={yTickFormatter} valueFormatter={valueFormatter} ariaLabel={`${title}监控趋势图`} />
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 border-t pt-3 text-[10px] text-muted-foreground">
          {series.map((item) => <span key={item.key} className="flex items-center gap-1.5"><span className="h-1.5 w-1.5 rounded-full" style={{ background: item.color }} />{item.label}<span className="text-muted-foreground/70">均 {valueFormatter(average(item.values))}</span></span>)}
        </div>
      </CardContent>
    </Card>
  );
}

function average(values: Array<number | null>): number {
  const valid = values.filter((value): value is number => value !== null && Number.isFinite(value));
  if (valid.length === 0) return 0;
  return valid.reduce((total, value) => total + value, 0) / valid.length;
}

function niceChartMax(value: number): number {
  const baseline = Math.max(value * 1.05, 1024);
  const units = [1, 1024, 1024 ** 2, 1024 ** 3];
  const unit = [...units].reverse().find((candidate) => baseline >= candidate) ?? 1;
  const normalized = baseline / unit;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10, 12, 15, 20, 25, 30, 40, 50, 60, 80, 100, 120, 150, 200, 250, 300, 400, 500, 600, 800, 1000];
  return (steps.find((step) => normalized <= step) ?? 1000) * unit;
}

function niceLinearMax(value: number): number {
  const baseline = Math.max(value * 1.1, 1);
  const exponent = 10 ** Math.floor(Math.log10(baseline));
  const normalized = baseline / exponent;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  return (steps.find((step) => normalized <= step) ?? 10) * exponent;
}

function InfoRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex min-w-0 items-start justify-between gap-4 border-b py-2.5 text-xs"><span className="shrink-0 text-muted-foreground">{label}</span><span className={cn("min-w-0 break-words text-right font-medium", mono && "font-mono text-[11px]")}>{value || "—"}</span></div>;
}

function InfoTile({ icon: Icon, label, value }: { icon: typeof Server; label: string; value: string }) {
  return <div className="rounded-lg border bg-background/55 p-3"><div className="flex items-center gap-1.5 text-[10px] text-muted-foreground"><Icon className="h-3 w-3" />{label}</div><p className="mt-1 text-sm font-semibold tabular-nums">{value}</p></div>;
}

