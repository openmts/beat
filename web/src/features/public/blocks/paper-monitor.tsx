import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Activity, ArrowLeft, Boxes, Cpu, HardDrive, LayoutDashboard, MemoryStick, Moon, Network, RefreshCw, Search, Server, Sun, Wifi } from "lucide-react";
import { api } from "@/api/client";
import type { PublicNode, SeriesPoint } from "@/api/types";
import { TrendChart, type TrendSeries } from "@/components/charts/trend-chart";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { NetworkProbeSection } from "@/features/nodes/network-probe-section";
import type { PublicThemeBlock } from "@/api/types";
import type { PublicHomeRuntime } from "@/features/public/renderer/public-runtime";
import { usePublicTheme } from "@/features/public/theme/public-theme";
import { formatBytes, formatPercent, formatRate, formatRelative, formatTime, formatUptime, statusLabel } from "@/lib/format";
import { cn } from "@/lib/utils";

type HomeBlockProps = { block: PublicThemeBlock; runtime: PublicHomeRuntime };
type Range = "1h" | "6h" | "24h" | "7d";
type DetailTab = "resources" | "network";

const RANGE_LABELS: Array<{ value: Range; label: string }> = [
  { value: "1h", label: "1 小时" },
  { value: "6h", label: "6 小时" },
  { value: "24h", label: "24 小时" },
  { value: "7d", label: "7 天" },
];

export function PaperMonitorBlock({ runtime }: HomeBlockProps) {
  const data = runtime as PublicHomeRuntime;
  const { resolvedMode, toggleMode } = usePublicTheme();
  const bootstrap = useQuery({ queryKey: ["public-bootstrap"], queryFn: api.publicBootstrap, refetchInterval: 30_000 });
  const [selectedId, setSelectedId] = useState("");
  const [mobileView, setMobileView] = useState<"list" | "detail">("list");
  const [range, setRange] = useState<Range>("6h");
  const [tab, setTab] = useState<DetailTab>("resources");
  const selected = data.nodes.find((node) => node.id === selectedId) ?? null;
  const siteName = bootstrap.data?.site.name || "Beat";
  const online = data.counts.online ?? data.nodes.filter((node) => node.status === "online").length;
  const total = data.counts.all ?? data.nodes.length;
  const query = data.query;

  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 821px)");
    function ensureSelection() {
      if (data.nodes.length === 0) {
        if (selectedId) setSelectedId("");
        return;
      }
      if (!data.nodes.some((node) => node.id === selectedId) && desktop.matches) {
        setSelectedId(data.nodes[0].id);
      }
    }
    ensureSelection();
    desktop.addEventListener("change", ensureSelection);
    return () => desktop.removeEventListener("change", ensureSelection);
  }, [data.nodes, selectedId]);

  useEffect(() => {
    if (selectedId && !data.nodes.some((node) => node.id === selectedId) && !window.matchMedia("(min-width: 821px)").matches) {
      setMobileView("list");
    }
  }, [data.nodes, selectedId]);

  function openNode(id: string) {
    setSelectedId(id);
    setMobileView("detail");
  }

  return (
    <div className="paper-home-shell">
      <div className="paper-app" data-mobile-view={mobileView}>
        <header className="paper-app-header">
          <div className="paper-brand">
            <span className="paper-brand-mark"><Activity size={16} /></span>
            <h1>{siteName}</h1>
            <span className="paper-mobile-badge"><i className={cn("paper-dot", online === 0 && "is-off")} />{online} / {total}</span>
          </div>
          <div className="paper-header-right">
            <div className="paper-summary-inline">
              <span className="paper-summary-nodes"><i className={cn("paper-dot", online === 0 && "is-off")} />在线 <b>{online}</b><small>/ {total}</small></span>
              <span className="paper-summary-stat">实时下行 <b>{formatRate(sum(data.nodes, "networkRxBps"))}</b></span>
              <span className="paper-summary-stat">实时上行 <b>{formatRate(sum(data.nodes, "networkTxBps"))}</b></span>
            </div>
            <div className="paper-header-actions">
              <Link to="/admin" className="paper-icon-button" aria-label="管理后台" title="管理后台"><LayoutDashboard size={16} /></Link>
              <button type="button" className="paper-icon-button" onClick={toggleMode} aria-label="切换主题" title="切换主题">
                {resolvedMode === "light" ? <Moon size={16} /> : <Sun size={16} />}
              </button>
              <button type="button" className={cn("paper-icon-button", data.nodesLoading && "paper-spinning")} onClick={data.refresh} aria-label="刷新" title="刷新"><RefreshCw size={16} /></button>
            </div>
          </div>
        </header>

        <div className="paper-toolbar">
          <div className="paper-filters" role="group" aria-label="节点分组筛选">
            <PaperFilter active={data.group === "all"} label="全部" count={data.counts.all ?? total} onClick={() => data.setGroup("all")} />
            {data.groups?.items.map((group) => <PaperFilter key={group.id} active={data.group === group.id} label={group.name} count={group.nodeCount} onClick={() => data.setGroup(group.id)} />)}
            {(data.groups?.ungroupedCount ?? 0) > 0 ? <PaperFilter active={data.group === "ungrouped"} label="未分组" count={data.groups?.ungroupedCount ?? 0} onClick={() => data.setGroup("ungrouped")} /> : null}
          </div>
          <div className="paper-tools">
            <label className="paper-search">
              <Search size={14} />
              <input value={query} onChange={(event) => data.setQuery(event.target.value)} placeholder="搜索节点" aria-label="搜索节点" />
            </label>
          </div>
        </div>

        <div className="paper-layout">
          <aside className="paper-list" aria-label="节点清单">
            {data.error ? <div className="paper-list-message"><ErrorState message={data.error} onRetry={data.refresh} /></div> : null}
            {!data.error && data.nodesLoading && data.nodes.length === 0 ? <div className="paper-list-message">正在读取节点…</div> : null}
            {!data.error && !data.nodesLoading && data.nodes.length === 0 ? <div className="paper-list-message"><EmptyState title="暂无节点" description="Agent 接入后，节点会自动出现在这里。" /></div> : null}
            {data.nodes.map((node) => <PaperNodeItem key={node.id} node={node} selected={node.id === selectedId} onClick={() => openNode(node.id)} />)}
          </aside>

          <main className="paper-detail">
            {selected ? <PaperNodeDetail node={selected} range={range} setRange={setRange} tab={tab} setTab={setTab} onBack={() => setMobileView("list")} /> : (
              <div className="paper-detail-empty"><Server size={24} /><p>从左侧选择一个节点查看指标</p><span>桌面端会自动选中第一个节点；移动端点击节点进入详情。</span></div>
            )}
          </main>
        </div>

        <footer className="paper-footer">
          <span className="paper-live"><i className="paper-dot" />实时更新中</span>
          <span>共 {total} 台，在线 {online} 台</span>
        </footer>
      </div>
    </div>
  );
}

function PaperFilter({ active, label, count, onClick }: { active: boolean; label: string; count: number; onClick: () => void }) {
  return <button type="button" className={cn("paper-filter", active && "is-active")} aria-pressed={active} onClick={onClick}>{label}<span>{count}</span></button>;
}

function PaperNodeItem({ node, selected, onClick }: { node: PublicNode; selected: boolean; onClick: () => void }) {
  const online = node.status !== "offline";
  const networkTotal = node.metrics.networkRxBps + node.metrics.networkTxBps;
  const rxShare = networkTotal > 0 ? node.metrics.networkRxBps / networkTotal * 100 : 0;
  const groupLabel = node.groupName || "未分组";
  return (
    <button type="button" className={cn("paper-node-item", selected && "is-selected", !online && "is-offline")} onClick={onClick}>
      <div className="paper-node-row">
        <span className="paper-node-name">{node.hostname}</span>
        <span className={cn("paper-node-status", node.status === "offline" && "is-bad")}><i className={cn("paper-dot", node.status !== "online" && "is-off")} />{statusLabel(node.status)}</span>
      </div>
      <div className="paper-node-meta">
        <span className="paper-node-code">{groupLabel.slice(0, 2).toUpperCase()}</span>
        <span>{node.os || "未知系统"} · {node.arch || "unknown"} · {groupLabel}</span>
      </div>
      <div className="paper-node-meters">
        <PaperMeter label="CPU" value={online ? formatPercent(node.metrics.cpuUsage, 0) : "—"} percent={online ? node.metrics.cpuUsage : 0} />
        <PaperMeter label="内存" value={online ? formatPercent(node.metrics.memoryUsage, 0) : "—"} percent={online ? node.metrics.memoryUsage : 0} />
        <PaperMeter label="磁盘" value={online ? formatPercent(node.metrics.diskUsage, 0) : "—"} percent={online ? node.metrics.diskUsage : 0} />
      </div>
      <div className="paper-node-traffic">
        <div><span>网络吞吐</span><b>{online ? `↓ ${formatRate(node.metrics.networkRxBps)} · ↑ ${formatRate(node.metrics.networkTxBps)}` : `最后上报 ${formatRelative(node.lastSeen)}`}</b></div>
        <div className="paper-bar"><i style={{ width: `${online ? Math.max(0, Math.min(100, rxShare)) : 0}%` }} /></div>
      </div>
    </button>
  );
}

function PaperMeter({ label, value, percent }: { label: string; value: string; percent: number }) {
  return <div className="paper-meter"><div><span>{label}</span><b>{value}</b></div><div className="paper-bar"><i className={cn(percent >= 90 && "is-bad", percent >= 75 && percent < 90 && "is-warn")} style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} /></div></div>;
}

function PaperNodeDetail({ node, range, setRange, tab, setTab, onBack }: {
  node: PublicNode;
  range: Range;
  setRange: (range: Range) => void;
  tab: DetailTab;
  setTab: (tab: DetailTab) => void;
  onBack: () => void;
}) {
  const metricsQuery = useQuery({
    queryKey: ["paper-node-metrics", node.id, range],
    queryFn: () => api.publicNodeMetrics(node.id, range),
    refetchInterval: 30_000,
  });
  const probesQuery = useQuery({
    queryKey: ["paper-node-probes", node.id, range],
    queryFn: () => api.publicNodeNetworkProbes(node.id, range),
    refetchInterval: 30_000,
  });
  const points = metricsQuery.data?.points ?? [];
  const online = node.status !== "offline";
  const networkTotal = node.metrics.networkRxBps + node.metrics.networkTxBps;
  const labels = Object.entries(node.labels);

  return (
    <>
      <button type="button" className="paper-back" onClick={onBack}><ArrowLeft size={15} />返回节点列表</button>
      <div className="paper-detail-head">
        <div className="paper-detail-title">
          <h2>{node.hostname}</h2>
          <p>{node.groupName || "未分组"} · {node.address || "地址未上报"} · 最近上报 {formatRelative(node.lastSeen)}</p>
        </div>
        <div className="paper-detail-head-meta">
          <span className={cn("paper-node-status", node.status === "offline" && "is-bad")}><i className={cn("paper-dot", node.status !== "online" && "is-off")} />{statusLabel(node.status)}</span>
          <span className="paper-agent">{node.arch || "unknown"} · 采集 {node.collectorIntervalSeconds}s</span>
        </div>
      </div>

      <div className="paper-stats">
        <PaperStat label="CPU" value={online ? formatPercent(node.metrics.cpuUsage) : "—"} sub={`${node.cpuCores} 线程 · 负载 ${node.metrics.load1.toFixed(2)}`} percent={online ? node.metrics.cpuUsage : 0} />
        <PaperStat label="内存" value={online ? formatPercent(node.metrics.memoryUsage, 0) : "—"} sub={online ? `${formatBytes(node.metrics.memoryUsedBytes)} / ${formatBytes(node.metrics.memoryTotalBytes || node.totalMemoryBytes)}` : formatBytes(node.metrics.memoryTotalBytes || node.totalMemoryBytes)} percent={online ? node.metrics.memoryUsage : 0} />
        <PaperStat label="磁盘" value={online ? formatPercent(node.metrics.diskUsage, 0) : "—"} sub={online ? `${formatBytes(node.metrics.diskUsedBytes)} / ${formatBytes(node.metrics.diskTotalBytes || node.totalDiskBytes)}` : formatBytes(node.metrics.diskTotalBytes || node.totalDiskBytes)} percent={online ? node.metrics.diskUsage : 0} />
        <PaperStat label="网络" value={online ? formatRate(networkTotal) : "—"} sub={online ? `↓ ${formatRate(node.metrics.networkRxBps)} · ↑ ${formatRate(node.metrics.networkTxBps)}` : `最后上报 ${formatRelative(node.lastSeen)}`} percent={online && networkTotal > 0 ? node.metrics.networkRxBps / networkTotal * 100 : 0} />
      </div>

      <PaperAvailability node={node} points={points} loading={metricsQuery.isPending} range={range} />

      <div className="paper-detail-tabs" role="tablist" aria-label="监控视图">
        <button type="button" role="tab" aria-selected={tab === "resources"} className={cn("paper-detail-tab", tab === "resources" && "is-active")} onClick={() => setTab("resources")}>系统资源</button>
        <button type="button" role="tab" aria-selected={tab === "network"} className={cn("paper-detail-tab", tab === "network" && "is-active")} onClick={() => setTab("network")}>网络监控</button>
      </div>

      <div className="paper-section-head">
        <span>{tab === "resources" ? "资源监控曲线" : "网络与探测曲线"}</span>
        <div className="paper-ranges" aria-label="时间范围">
          {RANGE_LABELS.map((item) => <button key={item.value} type="button" className={cn("paper-range", range === item.value && "is-active")} onClick={() => setRange(item.value)}>{item.label}</button>)}
        </div>
      </div>

      {metricsQuery.isPending ? <div className="paper-chart-loading">正在读取时序数据…</div> : null}
      {metricsQuery.isError ? <div className="paper-chart-loading is-error">时序数据读取失败：{metricsQuery.error.message}</div> : null}
      {!metricsQuery.isPending && !metricsQuery.isError && points.length === 0 ? <div className="paper-chart-loading">当前时间范围内暂无采集数据</div> : null}

      {tab === "resources" ? (
        <div className="paper-charts-grid">
          <PaperChartCard title="CPU 占用" description="总使用率、用户态与系统态" icon={Cpu} points={points} series={[
            { key: "cpu", label: "总使用率", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.cpuUsage) },
            { key: "user", label: "用户态", color: "hsl(var(--chart-memory))", values: points.map((point) => point.cpuUser) },
            { key: "system", label: "系统态", color: "hsl(var(--chart-disk))", values: points.map((point) => point.cpuSystem) },
          ]} yMax={100} yTicks={[100, 75, 50, 25, 0]} />
          <PaperChartCard title="内存使用" description="当前内存占用率" icon={MemoryStick} points={points} series={[
            { key: "memory", label: "内存", color: "hsl(var(--chart-memory))", values: points.map((point) => point.memoryUsage) },
          ]} yMax={100} yTicks={[100, 75, 50, 25, 0]} />
          <PaperChartCard title="磁盘使用" description="根分区使用率" icon={HardDrive} points={points} series={[
            { key: "disk", label: "磁盘", color: "hsl(var(--chart-disk))", values: points.map((point) => point.diskUsage) },
          ]} yMax={100} yTicks={[100, 75, 50, 25, 0]} />
          <PaperChartCard title="系统负载" description={`1 / 5 / 15 分钟 · ${node.cpuCores} 线程`} icon={Boxes} points={points} series={[
            { key: "load1", label: "1 分钟", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.load1) },
            { key: "load5", label: "5 分钟", color: "hsl(var(--chart-memory))", values: points.map((point) => point.load5) },
            { key: "load15", label: "15 分钟", color: "hsl(var(--chart-disk))", values: points.map((point) => point.load15) },
          ]} maxMode="linear" formatValue={(value) => value.toFixed(2)} />
        </div>
      ) : (
        <div className="paper-charts-grid">
          <PaperChartCard title="网络吞吐" description="实时下行与上行带宽" icon={Wifi} points={points} series={[
            { key: "rx", label: "下行", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.networkRxBps) },
            { key: "tx", label: "上行", color: "hsl(var(--chart-memory))", values: points.map((point) => point.networkTxBps) },
          ]} maxMode="rate" formatValue={formatRate} />
          <PaperChartCard title="连接与进程" description="TCP 连接数 / 进程数" icon={Network} points={points} series={[
            { key: "tcp", label: "TCP 连接", color: "hsl(var(--chart-cpu))", values: points.map((point) => point.tcpConnections) },
            { key: "processes", label: "进程", color: "hsl(var(--chart-memory))", values: points.map((point) => point.processes) },
          ]} maxMode="linear" formatValue={(value) => value.toFixed(0)} />
          {probesQuery.data && probesQuery.data.tasks.length > 0 ? <div className="paper-probes"><NetworkProbeSection data={probesQuery.data} /></div> : null}
        </div>
      )}

      <div className="paper-section-title">主机与系统配置</div>
      <div className="paper-info-grid">
        <div><span>操作系统</span><b>{node.os || "—"}</b></div>
        <div><span>内核版本</span><b className="paper-mono">{node.kernel || "—"}</b></div>
        <div><span>系统架构</span><b className="paper-mono">{node.arch || "—"}</b></div>
        <div><span>CPU 型号</span><b>{node.cpuModel || "—"}</b></div>
        <div><span>CPU 核心</span><b>{node.cpuCores} 线程</b></div>
        <div><span>内存总量</span><b>{formatBytes(node.metrics.memoryTotalBytes || node.totalMemoryBytes)}</b></div>
        <div><span>磁盘总量</span><b>{formatBytes(node.metrics.diskTotalBytes || node.totalDiskBytes)}</b></div>
        <div><span>运行时长</span><b>{formatUptime(node.metrics.uptimeSeconds)}</b></div>
        <div><span>首次接入</span><b>{formatTime(node.firstSeen)}</b></div>
        <div><span>采集间隔</span><b>{node.collectorIntervalSeconds} 秒</b></div>
        <div className="paper-info-labels"><span>节点标签</span><div>{labels.length > 0 ? labels.map(([key, value]) => <code key={key}>{key}={value}</code>) : <em>未配置</em>}</div></div>
      </div>
    </>
  );
}

function PaperStat({ label, value, sub, percent }: { label: string; value: string; sub: string; percent: number }) {
  return <div className="paper-stat"><span className="paper-stat-label">{label}</span><strong>{value}</strong><span className="paper-stat-sub">{sub}</span><div className="paper-bar"><i className={cn(percent >= 90 && "is-bad", percent >= 75 && percent < 90 && "is-warn")} style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} /></div></div>;
}

function PaperAvailability({ node, points, loading, range }: { node: PublicNode; points: SeriesPoint[]; loading: boolean; range: Range }) {
  const bars = useMemo(() => {
    if (points.length === 0) return Array.from({ length: 48 }, () => ({ tone: "empty", title: "暂无采集" }));
    const count = Math.min(72, Math.max(24, points.length));
    return Array.from({ length: count }, (_, index) => {
      const point = points[Math.floor(index / Math.max(1, count - 1) * (points.length - 1))];
      if (!point) return { tone: "empty", title: "暂无采集" };
      const hot = Math.max(point.cpuUsage, point.memoryUsage, point.diskUsage) >= 90;
      return { tone: hot ? "warn" : "up", title: `${formatTime(point.timestamp)} · CPU ${formatPercent(point.cpuUsage, 0)} · 内存 ${formatPercent(point.memoryUsage, 0)}` };
    });
  }, [points]);
  const active = bars.filter((bar) => bar.tone !== "empty").length;
  const coverage = bars.length > 0 ? Math.round(active / bars.length * 100) : 0;
  return (
    <div className="paper-uptime">
      <div className="paper-uptime-head"><span><i className={cn("paper-dot", node.status === "offline" && "is-off")} />采集覆盖（最近 {RANGE_LABELS.find((item) => item.value === range)?.label ?? range}）</span><strong>{loading ? "读取中" : `${coverage}%`}</strong></div>
      <div className="paper-uptime-bars" aria-label={`采集覆盖率 ${coverage}%`}>
        {bars.map((bar, index) => <i key={index} className={cn("paper-uptime-bar", `is-${bar.tone}`)} title={bar.title} />)}
      </div>
      <div className="paper-uptime-foot"><span>区间起点</span><span>当前</span></div>
    </div>
  );
}

function PaperChartCard({ title, description, icon: Icon, points, series, yMax, yTicks, maxMode = "percent", formatValue = formatPercent }: {
  title: string;
  description: string;
  icon: typeof Cpu;
  points: SeriesPoint[];
  series: TrendSeries[];
  yMax?: number;
  yTicks?: number[];
  maxMode?: "percent" | "linear" | "rate";
  formatValue?: (value: number) => string;
}) {
  const peak = Math.max(0, ...series.flatMap((item) => item.values).filter((value): value is number => value !== null && Number.isFinite(value)));
  const chartMax = yMax ?? (maxMode === "rate" ? niceRateMax(peak) : niceLinearMax(peak));
  const ticks = yTicks ?? [1, 0.75, 0.5, 0.25, 0].map((ratio) => ratio * chartMax);
  return (
    <div className="paper-chart-card">
      <div className="paper-chart-head">
        <div><h3><Icon size={13} />{title}</h3><p>{description}</p></div>
        <span>峰值 {points.length > 0 ? formatValue(peak) : "—"}</span>
      </div>
      <TrendChart timestamps={points.map((point) => point.timestamp)} series={series} height={182} yMax={chartMax} yTicks={ticks} valueFormatter={formatValue} yTickFormatter={(value) => maxMode === "rate" ? formatRate(value) : formatValue(value)} ariaLabel={`${title}监控趋势图`} />
      <div className="paper-chart-legend">{series.map((item) => <span key={item.key}><i style={{ background: item.color }} />{item.label}<b>{points.length > 0 ? formatValue(average(item.values)) : "—"}</b></span>)}</div>
    </div>
  );
}

function sum(nodes: PublicNode[], key: "networkRxBps" | "networkTxBps") {
  return nodes.reduce((total, node) => total + (node.status === "offline" ? 0 : node.metrics[key]), 0);
}

function average(values: Array<number | null>) {
  const valid = values.filter((value): value is number => value !== null && Number.isFinite(value));
  if (valid.length === 0) return 0;
  return valid.reduce((total, value) => total + value, 0) / valid.length;
}

function niceRateMax(value: number) {
  const baseline = Math.max(value * 1.1, 1024);
  const exponent = 1024 ** Math.floor(Math.log(baseline) / Math.log(1024));
  const normalized = baseline / exponent;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  return (steps.find((step) => normalized <= step) ?? 10) * exponent;
}

function niceLinearMax(value: number) {
  const baseline = Math.max(value * 1.1, 1);
  const exponent = 10 ** Math.floor(Math.log10(baseline));
  const normalized = baseline / exponent;
  const steps = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  return (steps.find((step) => normalized <= step) ?? 10) * exponent;
}
