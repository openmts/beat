import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Activity, ArrowRight, Cpu, Download, MemoryStick, Radio, RefreshCw, Server, ShieldAlert, Wifi } from "lucide-react";
import { api } from "@/api/client";
import { MetricCard } from "@/components/data/metric-card";
import { NodeTable } from "@/components/data/node-table";
import { StatusDonut } from "@/components/charts/status-donut";
import { TrendChart, type TrendSeries } from "@/components/charts/trend-chart";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Segmented } from "@/components/ui/segmented";
import { formatPercent, formatRate, formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

type Range = "1h" | "6h" | "24h" | "7d";
type SeriesKey = "cpu" | "memory" | "disk";

export function OverviewPage() {
  const [range, setRange] = useState<Range>("24h");
  const [visible, setVisible] = useState<Record<SeriesKey, boolean>>({ cpu: true, memory: true, disk: false });
  const overview = useQuery({ queryKey: ["overview", range], queryFn: () => api.overview(range), refetchInterval: 15_000 });

  if (overview.isPending) return <LoadingState label="正在读取节点指标…" />;
  if (overview.isError) return <ErrorState message={overview.error.message} onRetry={() => void overview.refetch()} />;
  const data = overview.data;
  const series: TrendSeries[] = [
    { key: "cpu", label: "CPU", color: "hsl(var(--chart-cpu))", values: data.fleet.map((point) => point.cpuUsage) },
    { key: "memory", label: "内存", color: "hsl(var(--chart-memory))", values: data.fleet.map((point) => point.memoryUsage) },
    { key: "disk", label: "磁盘", color: "hsl(var(--chart-disk))", values: data.fleet.map((point) => point.diskUsage) },
  ].filter((item) => visible[item.key as SeriesKey]);
  const timestamps = data.fleet.map((point) => point.timestamp);
  const totalNodes = Math.max(1, data.summary.nodeCount);

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Infrastructure observability"
        title="运行总览"
        description="以多值模型实时观察节点健康、资源水位与网络吞吐，数据由 Beat Agent 通过 gRPC 双向流上报。"
        actions={<>
          <Button variant="outline" size="sm" onClick={() => void overview.refetch()} disabled={overview.isFetching}><RefreshCw className={cn("h-3.5 w-3.5", overview.isFetching && "animate-spin")} />刷新</Button>
          <Link to="/admin/agents" className={buttonVariants({ size: "sm" })}><Download className="h-3.5 w-3.5" />接入节点</Link>
        </>}
      />

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="节点总数" value={data.summary.nodeCount} hint={`${data.summary.onlineCount + data.summary.warningCount} 个活跃 · ${data.summary.offlineCount} 个离线`} icon={Server} tone="primary" />
        <MetricCard label="平均 CPU" value={formatPercent(data.summary.avgCpu)} hint="在线节点算力水位" icon={Cpu} tone={data.summary.avgCpu > 80 ? "warning" : "success"} />
        <MetricCard label="平均内存" value={formatPercent(data.summary.avgMemory)} hint={`平均磁盘 ${formatPercent(data.summary.avgDisk)}`} icon={MemoryStick} tone={data.summary.avgMemory > 85 ? "warning" : "violet"} />
        <MetricCard label="高负载节点" value={data.summary.warningCount} hint={`下行 ${formatRate(data.summary.networkRx)}`} icon={ShieldAlert} tone={data.summary.warningCount > 0 ? "warning" : "success"} />
      </div>

      <div className="grid min-w-0 items-start gap-4 xl:grid-cols-[minmax(0,2.1fr)_minmax(300px,1fr)]">
        <Card className="min-w-0 overflow-hidden">
          <CardHeader className="flex-col gap-3 pb-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <CardTitle className="flex items-center gap-2"><Activity className="h-4 w-4 text-primary" />集群资源趋势</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">所有在线节点的多值指标聚合，按所选时间窗口降采样。</p>
            </div>
            <Segmented className="shrink-0 self-start" value={range} onChange={setRange} options={[{ value: "1h", label: "1H" }, { value: "6h", label: "6H" }, { value: "24h", label: "24H" }, { value: "7d", label: "7D" }]} />
          </CardHeader>
          <CardContent>
            <div className="mb-4 flex flex-wrap items-center gap-2">
              {([{ key: "cpu", label: "CPU 使用率", color: "bg-chart-cpu" }, { key: "memory", label: "内存使用率", color: "bg-chart-memory" }, { key: "disk", label: "磁盘使用率", color: "bg-chart-disk" }] as const).map((item) => (
                <button key={item.key} type="button" onClick={() => setVisible((current) => ({ ...current, [item.key]: !current[item.key] }))} className={cn("inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] transition", visible[item.key] ? "bg-card text-foreground" : "border-dashed text-muted-foreground opacity-60")}>
                  <span className={cn("h-1.5 w-1.5 rounded-full", item.color)} />{item.label}
                </button>
              ))}
              <span className="ml-auto text-[10px] text-muted-foreground">更新于 {formatTime(data.generatedAt)}</span>
            </div>
            <TrendChart timestamps={timestamps} series={series} height={270} />
          </CardContent>
        </Card>

        <div className="grid min-w-0 gap-4 sm:grid-cols-2 xl:grid-cols-1">
          <Card className="min-w-0 overflow-hidden">
            <CardHeader className="pb-2"><CardTitle>节点状态</CardTitle><p className="text-xs text-muted-foreground">连接状态与需关注节点分布。</p></CardHeader>
            <CardContent>
              <div className="flex min-w-0 items-center gap-4 sm:gap-5">
                <StatusDonut online={data.summary.onlineCount} warning={data.summary.warningCount} offline={data.summary.offlineCount} className="shrink-0" />
                <div className="min-w-0 flex-1 space-y-3">
                  <LegendRow color="bg-success" label="在线" value={data.summary.onlineCount} percent={data.summary.onlineCount / totalNodes * 100} />
                  <LegendRow color="bg-warning" label="需关注" value={data.summary.warningCount} percent={data.summary.warningCount / totalNodes * 100} />
                  <LegendRow color="bg-destructive" label="离线" value={data.summary.offlineCount} percent={data.summary.offlineCount / totalNodes * 100} />
                </div>
              </div>
              <div className="mt-5 grid grid-cols-2 gap-2">
                <MiniStat icon={Wifi} label="下行" value={formatRate(data.summary.networkRx)} />
                <MiniStat icon={Radio} label="上行" value={formatRate(data.summary.networkTx)} />
              </div>
            </CardContent>
          </Card>

          <Card className="min-w-0 overflow-hidden">
            <CardHeader className="flex-row items-center justify-between pb-2"><CardTitle>CPU 水位 Top 5</CardTitle><Badge variant="outline">实时</Badge></CardHeader>
            <CardContent className="space-y-3 pt-1">
              {data.topCpu.map((node) => (
                <Link key={node.id} to="/admin/nodes/$nodeId" params={{ nodeId: node.id }} className="group block">
                  <div className="mb-1 flex items-center justify-between gap-2 text-xs">
                    <span className="truncate font-medium group-hover:text-primary">{node.hostname}</span>
                    <span className={cn("tabular-nums", node.metrics.cpuUsage >= 85 ? "text-warning" : "text-muted-foreground")}>{formatPercent(node.metrics.cpuUsage)}</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-muted"><div className={cn("h-full rounded-full", node.metrics.cpuUsage >= 85 ? "bg-warning" : node.metrics.cpuUsage >= 60 ? "bg-primary" : "bg-success")} style={{ width: `${Math.min(100, node.metrics.cpuUsage)}%` }} /></div>
                </Link>
              ))}
            </CardContent>
          </Card>
        </div>
      </div>

      <div className="grid min-w-0 items-start gap-4 xl:grid-cols-[minmax(0,2.1fr)_minmax(320px,1fr)]">
        <Card className="min-w-0 overflow-hidden">
          <CardHeader className="flex-col gap-2 pb-2 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0"><CardTitle>节点概览</CardTitle><p className="mt-1 text-xs text-muted-foreground">按主机名排序，点击节点查看完整指标。</p></div>
            <Link to="/admin/nodes" className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline">全部节点 <ArrowRight className="h-3 w-3" /></Link>
          </CardHeader>
          <CardContent className="p-0"><NodeTable nodes={data.nodes.slice(0, 6)} compact admin /></CardContent>
        </Card>

        <Card className="min-w-0 overflow-hidden">
          <CardHeader className="flex-col gap-2 pb-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0"><CardTitle>采集概况</CardTitle><p className="mt-1 text-xs text-muted-foreground">Agent gRPC 长连接与节点接入状态。</p></div>
            <Link to="/admin/agents" className="text-xs font-medium text-primary hover:underline">节点接入</Link>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="rounded-lg border bg-background/55 p-4">
              <div className="flex items-center gap-2 text-xs font-semibold"><Radio className="h-4 w-4 text-primary" />gRPC 双向通道</div>
              <p className="mt-2 text-[11px] leading-5 text-muted-foreground">节点无需开放入站端口，Agent 按采集间隔向 Beat Server 推送多值指标。</p>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/45 px-3 py-2.5 text-[10px] text-muted-foreground"><span>当前活跃节点</span><span className="font-medium text-foreground">{data.nodes.filter((node) => node.status !== "offline").length} / {data.summary.nodeCount}</span></div>
            <div className="flex items-center justify-between rounded-lg bg-muted/45 px-3 py-2.5 text-[10px] text-muted-foreground"><span>采集间隔</span><span className="font-medium text-foreground">15 秒</span></div>
            <Link to="/admin/agents" className="flex items-center justify-between rounded-lg border border-primary/15 bg-primary/[0.04] px-3 py-2.5 text-[11px] font-medium text-primary transition-colors hover:bg-primary/[0.07]"><span>查看接入命令与令牌</span><ArrowRight className="h-3 w-3" /></Link>
          </CardContent>
        </Card>
      </div>

      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-xl border bg-card px-4 py-3 text-[11px] text-muted-foreground shadow-panel">
        <span className="flex items-center gap-2"><span className="h-1.5 w-1.5 rounded-full bg-success" />Beat Server 与 MTS 时序引擎运行正常</span>
        <span>数据刷新间隔 15s · 保留策略 7 天 · 多值模型存储</span>
      </div>
    </div>
  );
}

function LegendRow({ color, label, value, percent }: { color: string; label: string; value: number; percent: number }) {
  return (
    <div>
      <div className="flex items-center justify-between text-xs"><span className="flex items-center gap-1.5 text-muted-foreground"><span className={cn("h-1.5 w-1.5 rounded-full", color)} />{label}</span><span className="font-medium tabular-nums">{value}</span></div>
      <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted"><div className={cn("h-full rounded-full", color)} style={{ width: `${percent}%` }} /></div>
    </div>
  );
}

function MiniStat({ icon: Icon, label, value }: { icon: typeof Wifi; label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-background/55 px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground"><Icon className="h-3 w-3" />{label}</div>
      <p className="mt-1 truncate text-xs font-semibold tabular-nums">{value}</p>
    </div>
  );
}

