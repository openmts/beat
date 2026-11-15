import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Activity, ArrowLeft, Boxes, ChevronDown, Cpu, Fingerprint, HardDrive, MemoryStick, Network, RefreshCw, Server, Timer, Wifi } from "lucide-react";
import { api } from "@/api/client";
import { TrendChart, type TrendSeries } from "@/components/charts/trend-chart";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { MetricCard } from "@/components/data/metric-card";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Segmented } from "@/components/ui/segmented";
import { formatBytes, formatPercent, formatRate, formatRelative, formatUptime } from "@/lib/format";
import { groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";
import { NetworkProbeSection } from "./network-probe-section";

type Range = "1h" | "6h" | "24h" | "7d";

export function NodeDetailPage({ nodeId, admin = false }: { nodeId: string; admin?: boolean }) {
  const [range, setRange] = useState<Range>("24h");
  const node = useQuery({ queryKey: ["node", nodeId], queryFn: () => api.node(nodeId), refetchInterval: 15_000 });
  const metrics = useQuery({ queryKey: ["node-metrics", nodeId, range], queryFn: () => api.nodeMetrics(nodeId, range), refetchInterval: 30_000 });
  const networkProbes = useQuery({ queryKey: ["node-network-probes", nodeId, range], queryFn: () => api.nodeNetworkProbes(nodeId, range), refetchInterval: 30_000 });

  if (node.isPending) return <LoadingState label="正在读取节点详情…" />;
  if (node.isError) return <ErrorState message={node.error.message} onRetry={() => void node.refetch()} />;
  const data = node.data;
  const points = metrics.data?.points ?? [];
  const timestamps = points.map((point) => point.timestamp);
  const cpuSeries: TrendSeries[] = [
    { key: "cpu-total", label: "总使用率", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.cpuUsage) },
    { key: "cpu-user", label: "用户态", color: "hsl(var(--chart-memory))", values: points.map((point) => point.cpuUser) },
    { key: "cpu-system", label: "系统态", color: "hsl(var(--chart-disk))", values: points.map((point) => point.cpuSystem) },
  ];
  const memorySeries: TrendSeries[] = [
    { key: "memory", label: "内存使用率", color: "hsl(var(--chart-memory))", values: points.map((point) => point.memoryUsage) },
  ];
  const diskSeries: TrendSeries[] = [
    { key: "disk", label: "磁盘使用率", color: "hsl(var(--chart-disk))", values: points.map((point) => point.diskUsage) },
  ];
  const networkSeries: TrendSeries[] = [
    { key: "network-rx", label: "下行", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.networkRxBps) },
    { key: "network-tx", label: "上行", color: "hsl(var(--chart-network))", values: points.map((point) => point.networkTxBps) },
  ];
  const loadSeries: TrendSeries[] = [
    { key: "load-1", label: "1 分钟负载", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.load1) },
    { key: "load-5", label: "5 分钟负载", color: "hsl(var(--chart-memory))", values: points.map((point) => point.load5) },
    { key: "load-15", label: "15 分钟负载", color: "hsl(var(--chart-disk))", values: points.map((point) => point.load15) },
  ];
  const networkYMax = niceChartMax(points.reduce((maximum, point) => Math.max(maximum, point.networkRxBps, point.networkTxBps), 0));
  const loadYMax = niceLinearMax(points.reduce((maximum, point) => Math.max(maximum, point.load1, point.load5, point.load15), 0));
  const group = data.groupId ? groupColor(data.groupColor) : null;

  return (
    <div className="space-y-5 animate-fade-in">
      {admin ? (
        <Link to="/admin/nodes" className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"><ArrowLeft className="h-3.5 w-3.5" />返回节点管理</Link>
      ) : (
        <Link to="/" className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"><ArrowLeft className="h-3.5 w-3.5" />返回节点广场</Link>
      )}
      <PageHeader
        eyebrow="Node detail"
        title={data.hostname}
        description={`${data.address || "未上报地址"} · ${group ? data.groupName : "未分组"}`}
        actions={<><StatusBadge status={data.status} className="h-7 px-3" /><Button variant="outline" size="sm" onClick={() => { void node.refetch(); void metrics.refetch(); void networkProbes.refetch(); }}><RefreshCw className="h-3.5 w-3.5" />刷新</Button>{admin ? <Link to="/admin/agents" className={buttonVariants({ variant: "outline", size: "sm" })}><Server className="h-3.5 w-3.5" />节点接入</Link> : null}</>}
      />

      {data.status === "offline" ? <div className="rounded-xl border border-destructive/20 bg-destructive/[0.06] px-4 py-3 text-xs text-destructive">该节点已超过离线判定窗口未上报，最后通信时间为 {formatRelative(data.lastSeen)}。请检查 Agent 进程和网络连通性。</div> : null}
      {data.status === "warning" ? <div className="rounded-xl border border-warning/25 bg-warning/[0.06] px-4 py-3 text-xs text-warning">节点仍在上报，但至少一项资源使用率超过 90%，建议检查 CPU、内存或磁盘负载。</div> : null}

      <Card className="overflow-hidden">
        <CardHeader className="border-b bg-gradient-to-r from-card to-primary/[0.025]">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <CardTitle className="flex items-center gap-2"><Server className="h-4 w-4 text-primary" />节点概况</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">主机环境、分组标签与接入状态集中在同一视图。</p>
            </div>
            <span className="inline-flex items-center gap-1.5 self-start rounded-md bg-muted/55 px-2 py-1 text-[10px] text-muted-foreground">
              <span className={cn("h-1.5 w-1.5 rounded-full", data.status === "online" ? "bg-success" : data.status === "warning" ? "bg-warning" : "bg-destructive")} />最近上报 {formatRelative(data.lastSeen)}
            </span>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <div className="grid divide-y xl:grid-cols-[minmax(0,1.55fr)_minmax(280px,1fr)_minmax(250px,0.9fr)] xl:divide-x xl:divide-y-0">
            <section className="p-5">
              <div className="flex items-center gap-2"><Cpu className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">系统与运行环境</h2></div>
              <p className="mt-1 text-[11px] text-muted-foreground">Agent 上报的主机、内核与采集配置。</p>
              <div className="mt-3 grid gap-x-6 sm:grid-cols-2">
                <CompactInfoRow label="操作系统" value={data.os} />
                <CompactInfoRow label="内核版本" value={data.kernel} mono />
                <CompactInfoRow label="CPU 型号" value={data.cpuModel} />
                <CompactInfoRow label="CPU 核心" value={`${data.cpuCores} 线程`} />
                <CompactInfoRow label="架构" value={data.arch} mono />
                <CompactInfoRow label="采集间隔" value={`${data.collectorIntervalSeconds}s`} />
                <CompactInfoRow label="Agent 版本" value={data.agentVersion} mono />
              </div>
            </section>

            <section className="p-5">
              <div className="flex items-center gap-2"><Boxes className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">分组与标签</h2></div>
              <p className="mt-1 text-[11px] text-muted-foreground">用于节点广场的归类与检索。</p>
              <div className="mt-3 flex items-center justify-between gap-3 rounded-lg border bg-background/55 p-3">
                <div className="min-w-0">
                  <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">所属分组</p>
                  <p className="mt-1 truncate text-sm font-semibold">{group ? data.groupName : "未分组"}</p>
                </div>
                <span className={cn("inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2 py-1 text-[10px] font-medium", group?.chip ?? "border-dashed bg-muted/35 text-muted-foreground")}>
                  <span className={cn("h-1.5 w-1.5 rounded-full", group?.dot ?? "bg-muted-foreground/50")} />
                  {group ? "已纳管" : "待分组"}
                </span>
              </div>
              <div className="mt-4">
                <p className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">业务标签</p>
                <div className="flex flex-wrap gap-1.5">
                  {Object.entries(data.labels).map(([key, value]) => <Badge key={key} variant="outline" className="font-mono text-[10px]">{key}={value}</Badge>)}
                  {Object.keys(data.labels).length === 0 ? <span className="text-xs text-muted-foreground">未配置标签</span> : null}
                </div>
              </div>
            </section>

            <section className="p-5">
              <div className="flex items-center gap-2"><Activity className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">接入与运行</h2></div>
              <p className="mt-1 text-[11px] text-muted-foreground">上报时效与进程连接快照。</p>
              <div className="mt-3 grid grid-cols-2 gap-2">
                <InfoTile icon={Wifi} label="首次接入" value={formatRelative(data.firstSeen)} />
                <InfoTile icon={Timer} label="运行时长" value={formatUptime(data.metrics.uptimeSeconds)} />
                <InfoTile icon={Activity} label="进程数" value={String(data.metrics.processes)} />
                <InfoTile icon={Network} label="TCP 连接" value={String(data.metrics.tcpConnections)} />
              </div>
              <div className="mt-3 rounded-lg border bg-background/55 p-3">
                <div className="flex items-center justify-between text-[10px] text-muted-foreground"><span>当前系统负载</span><span>{data.cpuCores} 线程</span></div>
                <div className="mt-2 grid grid-cols-3 gap-2 text-center">
                  {[['1 分钟', data.metrics.load1], ['5 分钟', data.metrics.load5], ['15 分钟', data.metrics.load15]].map(([label, value]) => (
                    <div key={String(label)} className="rounded-md bg-muted/45 px-2 py-2">
                      <p className="text-[9px] text-muted-foreground">{label}</p>
                      <p className="mt-0.5 text-sm font-semibold tabular-nums">{Number(value).toFixed(2)}</p>
                    </div>
                  ))}
                </div>
              </div>
            </section>
          </div>

          <details className="group border-t">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-5 py-3 text-[11px] font-medium text-muted-foreground transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
              <span className="flex items-center gap-2"><Fingerprint className="h-3.5 w-3.5" />技术标识</span>
              <span className="flex items-center gap-1.5 text-[10px]">诊断与 Agent 对接时使用<ChevronDown className="h-3.5 w-3.5 transition-transform group-open:rotate-180" /></span>
            </summary>
            <div className="grid gap-3 px-5 pb-5 sm:grid-cols-2">
              <div className="rounded-lg bg-muted/40 p-3">
                <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Node ID</p>
                <p className="mt-1 break-all font-mono text-[10px] leading-5 text-foreground/80">{data.id}</p>
              </div>
              <div className="rounded-lg bg-muted/40 p-3">
                <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Agent ID</p>
                <p className="mt-1 break-all font-mono text-[10px] leading-5 text-foreground/80">{data.agentId}</p>
              </div>
            </div>
          </details>
        </CardContent>
      </Card>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="CPU 使用率" value={formatPercent(data.metrics.cpuUsage)} hint={`${data.cpuCores} 线程 · 负载 ${data.metrics.load1.toFixed(2)}`} icon={Cpu} tone={data.metrics.cpuUsage >= 85 ? "warning" : "success"} />
        <MetricCard label="内存使用率" value={formatPercent(data.metrics.memoryUsage)} hint={`${formatBytes(data.metrics.memoryUsedBytes)} / ${formatBytes(data.metrics.memoryTotalBytes)}`} icon={MemoryStick} tone={data.metrics.memoryUsage >= 85 ? "warning" : "violet"} />
        <MetricCard label="磁盘使用率" value={formatPercent(data.metrics.diskUsage)} hint={`${formatBytes(data.metrics.diskUsedBytes)} / ${formatBytes(data.metrics.diskTotalBytes)}`} icon={HardDrive} tone={data.metrics.diskUsage >= 85 ? "warning" : "primary"} />
        <MetricCard label="网络吞吐" value={formatRate(data.metrics.networkRxBps + data.metrics.networkTxBps)} hint={`下行 ${formatRate(data.metrics.networkRxBps)} · 上行 ${formatRate(data.metrics.networkTxBps)}`} icon={Wifi} tone="success" />
      </div>

      <section className="space-y-3">
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-panel sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-sm font-semibold"><Activity className="h-4 w-4 text-primary" />资源监控视图</h2>
            <p className="mt-1 text-[11px] text-muted-foreground">CPU、内存、磁盘与网络上下行基于 MTS 多值时序数据绘制 · 采样步长 {metrics.data?.stepSeconds ? `${Math.round(metrics.data.stepSeconds / 60)} 分钟` : "—"}</p>
          </div>
          <Segmented value={range} onChange={setRange} options={[{ value: "1h", label: "1H" }, { value: "6h", label: "6H" }, { value: "24h", label: "24H" }, { value: "7d", label: "7D" }]} />
        </div>
        {metrics.isPending ? <Card><CardContent><LoadingState label="正在查询时序数据…" /></CardContent></Card> : metrics.isError ? <Card><CardContent><ErrorState message={metrics.error.message} onRetry={() => void metrics.refetch()} /></CardContent></Card> : (
          <div className="grid gap-4 lg:grid-cols-2">
            <MetricTrendCard title="CPU 使用率" description={`总使用率与用户态、系统态分解 · ${data.cpuCores} 线程`} icon={Cpu} series={cpuSeries} timestamps={timestamps} />
            <MetricTrendCard title="内存使用率" description={`${formatBytes(data.metrics.memoryUsedBytes)} / ${formatBytes(data.metrics.memoryTotalBytes)}`} icon={MemoryStick} series={memorySeries} timestamps={timestamps} />
            <MetricTrendCard title="磁盘使用率" description={`${formatBytes(data.metrics.diskUsedBytes)} / ${formatBytes(data.metrics.diskTotalBytes)}`} icon={HardDrive} series={diskSeries} timestamps={timestamps} />
            <MetricTrendCard title="网络吞吐" description={`当前下行 ${formatRate(data.metrics.networkRxBps)} · 上行 ${formatRate(data.metrics.networkTxBps)}`} icon={Wifi} series={networkSeries} timestamps={timestamps} yMax={networkYMax} yTicks={[1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * networkYMax)} yTickFormatter={formatRate} valueFormatter={formatRate} />
            <MetricTrendCard className="lg:col-span-2" title="系统负载" description={`1 / 5 / 15 分钟 Load Average · ${data.cpuCores} 线程`} icon={Boxes} series={loadSeries} timestamps={timestamps} yMax={loadYMax} yTicks={[1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * loadYMax)} yTickFormatter={(value) => value.toFixed(value < 10 ? 1 : 0)} valueFormatter={(value) => value.toFixed(2)} />
          </div>
        )}
      </section>

      {networkProbes.isSuccess && networkProbes.data && networkProbes.data.tasks.length > 0 ? <NetworkProbeSection data={networkProbes.data} /> : null}

    </div>
  );
}

function MetricTrendCard({
  title,
  description,
  icon: Icon,
  series,
  timestamps,
  yMax = 100,
  yTicks,
  yTickFormatter,
  valueFormatter = formatPercent,
  className,
}: {
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
          <div className="shrink-0 text-right">
            <p className="text-[9px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">区间峰值</p>
            <p className="mt-1 text-sm font-semibold tabular-nums">{valueFormatter(peak)}</p>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-1">
        <TrendChart timestamps={timestamps} series={series} height={230} yMax={yMax} yTicks={yTicks} yTickFormatter={yTickFormatter} valueFormatter={valueFormatter} ariaLabel={`${title}监控趋势图`} />
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 border-t pt-3 text-[10px] text-muted-foreground">
          {series.map((item) => (
            <span key={item.key} className="flex items-center gap-1.5">
              <span className="h-1.5 w-1.5 rounded-full" style={{ background: item.color }} />
              {item.label}<span className="text-muted-foreground/70">均 {valueFormatter(average(item.values))}</span>
            </span>
          ))}
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

function CompactInfoRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex min-w-0 items-start justify-between gap-4 border-b py-2.5 text-xs"><span className="shrink-0 text-muted-foreground">{label}</span><span className={cn("min-w-0 break-words text-right font-medium", mono && "font-mono text-[11px]")}>{value || "—"}</span></div>;
}

function InfoTile({ icon: Icon, label, value }: { icon: typeof Server; label: string; value: string }) {
  return <div className="rounded-lg border bg-background/55 p-3"><div className="flex items-center gap-1.5 text-[10px] text-muted-foreground"><Icon className="h-3 w-3" />{label}</div><p className="mt-1 text-sm font-semibold tabular-nums">{value}</p></div>;
}

