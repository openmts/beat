import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Cpu, HardDrive, MemoryStick } from "lucide-react";
import type { PublicNode } from "@/api/types";
import { StatusBadge } from "@/components/feedback/status-badge";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { formatPercent, formatRate, formatRelative } from "@/lib/format";
import { groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";

export function NodeCard({ node }: { node: PublicNode }) {
  const labels = Object.entries(node.labels);
  const visibleLabels = labels.slice(0, 3);
  const group = node.groupId ? groupColor(node.groupColor) : null;

  return (
    <Link to="/nodes/$nodeId" params={{ nodeId: node.id }} className="group block min-w-0 rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background">
      <Card className="h-full overflow-hidden transition-all duration-200 group-hover:-translate-y-0.5 group-hover:border-primary/25 group-hover:shadow-float">
        <div className="flex items-start justify-between gap-4 border-b bg-gradient-to-r from-card to-primary/[0.025] p-4 sm:p-5">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="truncate text-[15px] font-semibold tracking-tight group-hover:text-primary">{node.hostname}</h2>
              <ArrowUpRight className="h-3.5 w-3.5 shrink-0 text-primary opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
            <p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{node.address || "地址未上报"}</p>
            <p className="mt-1 truncate text-[11px] text-muted-foreground">{node.os || "未知系统"} · {node.arch || "unknown"}</p>
            <span className={cn("mt-2 inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-[9px] font-medium", group?.chip ?? "border-dashed bg-muted/35 text-muted-foreground")}>
              <span className={cn("h-1.5 w-1.5 rounded-full", group?.dot ?? "bg-muted-foreground/50")} />
              {group ? node.groupName : "未分组"}
            </span>
          </div>
          <StatusBadge status={node.status} />
        </div>

        <div className="space-y-4 p-4 sm:p-5">
          <div className="grid gap-3">
            <MetricLine icon={Cpu} label="CPU" value={node.metrics.cpuUsage} />
            <MetricLine icon={MemoryStick} label="内存" value={node.metrics.memoryUsage} />
            <MetricLine icon={HardDrive} label="磁盘" value={node.metrics.diskUsage} />
          </div>

          <div className="grid grid-cols-2 gap-2 rounded-lg bg-muted/45 p-3 text-[11px]">
            <div>
              <p className="text-[9px] uppercase tracking-wide text-muted-foreground">下行</p>
              <p className="mt-1 font-medium tabular-nums">↓ {formatRate(node.metrics.networkRxBps)}</p>
            </div>
            <div>
              <p className="text-[9px] uppercase tracking-wide text-muted-foreground">上行</p>
              <p className="mt-1 font-medium tabular-nums">↑ {formatRate(node.metrics.networkTxBps)}</p>
            </div>
          </div>

          <div className="flex min-h-5 flex-wrap items-center gap-1.5">
            {visibleLabels.map(([key, value]) => <span key={key} className="rounded-md border bg-background/70 px-1.5 py-0.5 font-mono text-[9px] text-muted-foreground">{key}={value}</span>)}
            {labels.length > visibleLabels.length ? <span className="text-[9px] text-muted-foreground">+{labels.length - visibleLabels.length}</span> : null}
            {labels.length === 0 ? <span className="text-[10px] text-muted-foreground">未配置标签</span> : null}
          </div>

          <div className="flex items-center justify-between border-t pt-3 text-[10px] text-muted-foreground">
            <span>采集间隔 {node.collectorIntervalSeconds}s</span>
            <span>最后上报 {formatRelative(node.lastSeen)}</span>
          </div>
        </div>
      </Card>
    </Link>
  );
}

function MetricLine({ icon: Icon, label, value }: { icon: typeof Cpu; label: string; value: number }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between text-[11px]">
        <span className="flex items-center gap-1.5 text-muted-foreground"><Icon className="h-3 w-3" />{label}</span>
        <span className={cn("font-medium tabular-nums", value >= 90 ? "text-warning" : "text-foreground")}>{formatPercent(value)}</span>
      </div>
      <Progress value={value} indicatorClassName={value >= 90 ? "bg-warning" : value >= 70 ? "bg-primary" : "bg-success"} />
    </div>
  );
}
