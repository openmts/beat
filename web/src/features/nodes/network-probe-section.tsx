import { Activity, ArrowDownUp, Network, Timer } from "lucide-react";
import type {
  Carrier,
  NetworkProbeTargetSeries,
  NetworkProbeTaskSeries,
  NodeNetworkProbesResponse,
} from "@/api/types";
import { TrendChart, type TrendSeries } from "@/components/charts/trend-chart";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatRelative } from "@/lib/format";
import { cn } from "@/lib/utils";

const CARRIERS: Record<Carrier, { name: string; short: string; color: string; dot: string }> = {
  telecom: { name: "中国电信", short: "电信", color: "hsl(var(--chart-cpu))", dot: "bg-cyan-500" },
  unicom: { name: "中国联通", short: "联通", color: "hsl(var(--chart-memory))", dot: "bg-rose-500" },
  mobile: { name: "中国移动", short: "移动", color: "hsl(var(--chart-network))", dot: "bg-emerald-500" },
};

export function NetworkProbeSection({ data }: { data: NodeNetworkProbesResponse }) {
  const tasks = data.tasks;
  return (
    <section className="space-y-3">
      <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 shadow-panel sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold"><Network className="h-4 w-4 text-primary" />三网质量监控</h2>
          <p className="mt-1 text-[11px] text-muted-foreground">来自任务中心的三网 TCP Connect 探测 · 时延与丢包独立写入 MTS 多值时序</p>
        </div>
        <Badge variant="outline" className="w-fit">{tasks.length} 个监控任务</Badge>
      </div>
      {tasks.map((task) => <NetworkProbeTaskCard key={task.taskId} task={task} />)}
    </section>
  );
}

function NetworkProbeTaskCard({ task }: { task: NetworkProbeTaskSeries }) {
  const hasData = task.timestamps.length > 0 && task.targets.length > 0;
  const latencyPeak = Math.max(0, ...task.targets.flatMap((target) => target.latencyAvgMs).filter((value): value is number => value !== null && Number.isFinite(value)));
  const latencyYMax = probeLatencyMax(latencyPeak);
  const latencySeries: TrendSeries[] = task.targets.map((target) => ({
    key: `latency-${target.carrier}`,
    label: CARRIERS[target.carrier].short,
    color: CARRIERS[target.carrier].color,
    values: target.latencyAvgMs,
  }));
  const lossSeries: TrendSeries[] = task.targets.map((target) => ({
    key: `loss-${target.carrier}`,
    label: CARRIERS[target.carrier].short,
    color: CARRIERS[target.carrier].color,
    values: target.lossPercent,
  }));
  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b bg-muted/[0.12] pb-3">
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="flex items-center gap-2"><ArrowDownUp className="h-4 w-4 text-primary" />{task.taskName}</CardTitle>
          <Badge variant="default" className="px-1.5 py-0 text-[8px]">TCP Connect</Badge>
          <span className="ml-auto text-[9px] text-muted-foreground">{hasData ? `最近采集 ${formatRelative(task.lastRunAt)}` : "任务已启用，等待首次采集"}</span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        {!hasData ? <div className="rounded-xl border border-dashed bg-muted/[0.08] px-4 py-8 text-center"><Activity className="mx-auto h-5 w-5 text-muted-foreground/60" /><p className="mt-2 text-[11px] font-medium">尚未收到该任务的三网探测结果</p><p className="mt-1 text-[9px] text-muted-foreground">Agent 下一次任务执行完成后，时延与丢包趋势会显示在这里。</p></div> : <>
        <div className="grid gap-2.5 md:grid-cols-3">
          {task.targets.map((target) => <ProbeTargetSummary key={target.carrier} target={target} />)}
        </div>
        <div className="grid gap-3 xl:grid-cols-2">
          <ProbeTrendCard title="三网时延" description="各运营商 TCP 建连平均 RTT · 丢包区间显示为空值" icon={Timer} series={latencySeries} timestamps={task.timestamps} yMax={latencyYMax} yTicks={[1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * latencyYMax)} yTickFormatter={(value) => `${formatNumber(value)}ms`} valueFormatter={formatLatencyValue} />
          <ProbeTrendCard title="三网丢包率" description="超时/无响应计入丢包，连接拒绝与 DNS 失败单独判定" icon={Network} series={lossSeries} timestamps={task.timestamps} yMax={100} yTicks={[100, 75, 50, 25, 0]} yTickFormatter={(value) => `${Math.round(value)}%`} valueFormatter={formatLossValue} />
        </div>
        </>}
      </CardContent>
    </Card>
  );
}

function ProbeTargetSummary({ target }: { target: NetworkProbeTargetSeries }) {
  const meta = CARRIERS[target.carrier];
  const latestIndex = target.latencyAvgMs.length - 1;
  const latency = latestIndex >= 0 ? target.latencyAvgMs[latestIndex] : null;
  const loss = latestIndex >= 0 ? target.lossPercent[latestIndex] : null;
  const p95 = latestIndex >= 0 ? target.latencyP95Ms[latestIndex] : null;
  const jitter = latestIndex >= 0 ? target.latencyJitterMs[latestIndex] : null;
  const warning = (loss !== null && loss > 0) || latency === null;
  return (
    <div className={cn("rounded-xl border bg-background/55 p-3", warning ? "border-warning/20" : "border-border")}>
      <div className="flex min-w-0 items-center gap-2">
        <span className={cn("h-2 w-2 shrink-0 rounded-full", meta.dot)} />
        <p className="text-[11px] font-semibold">{meta.name}</p>
        <span className="ml-auto truncate text-[8px] text-muted-foreground">{target.label || "TCP 目标"}</span>
      </div>
      <div className="mt-2.5 grid grid-cols-2 gap-1.5">
        <ProbeSummaryMetric label="平均时延" value={formatLatencyValue(latency)} tone={latency === null ? "muted" : warning ? "warning" : "success"} />
        <ProbeSummaryMetric label="丢包率" value={formatLossValue(loss)} tone={loss === null ? "muted" : loss > 0 ? "warning" : "success"} />
        <ProbeSummaryMetric label="P95 时延" value={formatLatencyValue(p95)} tone={p95 === null ? "muted" : "default"} />
        <ProbeSummaryMetric label="抖动" value={formatLatencyValue(jitter)} tone={jitter === null ? "muted" : "default"} />
      </div>
    </div>
  );
}

function ProbeSummaryMetric({ label, value, tone }: { label: string; value: string; tone: "success" | "warning" | "muted" | "default" }) {
  return <div className="rounded-lg bg-muted/45 px-2 py-1.5"><p className="text-[7px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">{label}</p><p className={cn("mt-0.5 truncate text-[11px] font-semibold tabular-nums", tone === "success" && "text-success", tone === "warning" && "text-warning", tone === "muted" && "text-muted-foreground")}>{value}</p></div>;
}

function ProbeTrendCard({
  title,
  description,
  icon: Icon,
  series,
  timestamps,
  yMax,
  yTicks,
  yTickFormatter,
  valueFormatter,
}: {
  title: string;
  description: string;
  icon: typeof Timer;
  series: TrendSeries[];
  timestamps: string[];
  yMax: number;
  yTicks: number[];
  yTickFormatter: (value: number) => string;
  valueFormatter: (value: number) => string;
}) {
  const values = series.flatMap((item) => item.values).filter((value): value is number => value !== null && Number.isFinite(value));
  const peak = values.length > 0 ? Math.max(...values) : null;
  return (
    <div className="rounded-xl border bg-background/45 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0"><p className="flex items-center gap-1.5 text-[11px] font-semibold"><Icon className="h-3.5 w-3.5 text-primary" />{title}</p><p className="mt-0.5 text-[9px] text-muted-foreground">{description}</p></div>
        <div className="shrink-0 text-right"><p className="text-[7px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">区间峰值</p><p className="mt-0.5 text-[11px] font-semibold tabular-nums">{peak === null ? "不可测" : valueFormatter(peak)}</p></div>
      </div>
      <TrendChart className="mt-1" timestamps={timestamps} series={series} height={205} yMax={yMax} yTicks={yTicks} yTickFormatter={yTickFormatter} valueFormatter={valueFormatter} ariaLabel={`${title}监控趋势图`} />
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t pt-2 text-[9px] text-muted-foreground">
        {series.map((item) => <span key={item.key} className="flex items-center gap-1.5"><span className="h-1.5 w-1.5 rounded-full" style={{ background: item.color }} />{item.label}</span>)}
      </div>
    </div>
  );
}

function formatLatencyValue(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return "不可测";
  return `${formatNumber(value)} ms`;
}

function formatLossValue(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return "不可用";
  return `${formatNumber(value)}%`;
}

function formatNumber(value: number): string {
  return value.toFixed(value < 10 ? 2 : 1);
}

function probeLatencyMax(value: number): number {
  const baseline = Math.max(value * 1.15, 10);
  const exponent = 10 ** Math.floor(Math.log10(baseline));
  const normalized = baseline / exponent;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  return (steps.find((step) => normalized <= step) ?? 10) * exponent;
}
