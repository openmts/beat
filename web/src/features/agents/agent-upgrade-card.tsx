import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  DownloadCloud,
  ExternalLink,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ServerCog,
  XCircle,
} from "lucide-react";
import type { AgentUpgradeBatch, AgentUpgradeItem, AgentUpgradeItemStatus } from "@/api/types";
import { api } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Modal } from "@/components/ui/modal";
import { Progress } from "@/components/ui/progress";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

const itemStatusText: Record<AgentUpgradeItemStatus, string> = {
  queued: "排队中",
  delivered: "已下发",
  installing: "安装中",
  restarting: "等待重连",
  succeeded: "已完成",
  failed: "失败",
};

const itemStatusTone: Record<AgentUpgradeItemStatus, "default" | "success" | "warning" | "destructive" | "secondary"> = {
  queued: "secondary",
  delivered: "default",
  installing: "warning",
  restarting: "warning",
  succeeded: "success",
  failed: "destructive",
};

export function AgentUpgradeCard() {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [includeOffline, setIncludeOffline] = useState(true);
  const [batchID, setBatchID] = useState<string | null>(null);
  const overview = useQuery({
    queryKey: ["agent-upgrade-overview"],
    queryFn: api.agentUpgradeOverview,
    refetchInterval: (query) => query.state.data?.activeBatch ? 2_000 : 15_000,
  });
  const activeBatchID = batchID ?? overview.data?.activeBatch?.id ?? null;
  const batchQuery = useQuery({
    queryKey: ["agent-upgrade-batch", activeBatchID],
    queryFn: () => api.agentUpgradeBatch(activeBatchID!),
    enabled: Boolean(activeBatchID),
    refetchInterval: (query) => query.state.data?.status === "running" ? 2_000 : false,
  });
  const createMutation = useMutation({
    mutationFn: () => api.createAgentUpgrade(includeOffline),
    onSuccess: (response) => {
      setBatchID(response.batch.id);
      setOpen(true);
      void queryClient.invalidateQueries({ queryKey: ["agent-upgrade-overview"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-upgrade-batch"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });
  const retryMutation = useMutation({
    mutationFn: () => api.retryAgentUpgrade(activeBatchID!),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["agent-upgrade-overview"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-upgrade-batch", activeBatchID] });
    },
  });

  const batch = batchQuery.data ?? overview.data?.activeBatch ?? null;
  const running = batch?.status === "running";
  const canStart = Boolean(overview.data && overview.data.upgradable > 0 && !overview.data.activeBatch);
  const latest = overview.data?.recentBatches.find((item) => item.status !== "running");
  const latestFailed = Boolean(latest && latest.failed > 0);
  const actionLabel = running ? "查看升级进度" : latestFailed ? "查看失败任务" : canStart ? "一键升级全部" : latest ? "查看最近任务" : "一键升级全部";
  const actionEnabled = running || canStart || Boolean(latest);

  useEffect(() => {
    if (batch && batch.status !== "running") {
      void queryClient.invalidateQueries({ queryKey: ["agent-upgrade-overview"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
    }
  }, [batch?.status, queryClient]);

  function openDialog() {
    createMutation.reset();
    retryMutation.reset();
    const historyID = latestFailed || !canStart ? latest?.id : null;
    setBatchID(overview.data?.activeBatch?.id ?? historyID ?? null);
    setOpen(true);
  }

  return (
    <>
      <Card data-testid="agent-upgrade-card">
        <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><ServerCog className="h-4 w-4 text-primary" />Agent 版本升级</CardTitle>
            <p className="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">升级指令由 Server 主动下发，Agent 收到后从 GitHub Release 下载目标版本，不会定时检查更新。在线节点立即执行，离线节点将在重新接入后继续。</p>
          </div>
          <Button size="sm" onClick={openDialog} disabled={!actionEnabled} className="w-full sm:w-auto" data-testid="agent-upgrade-open">
            {running ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <DownloadCloud className="h-3.5 w-3.5" />}
            {actionLabel}
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          {overview.isPending ? <div className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle className="h-3.5 w-3.5 animate-spin" />正在读取版本分布…</div> : null}
          {overview.isError ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-xs text-destructive">{overview.error.message}</div> : null}
          {overview.data ? <>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <Summary label="目标版本" value={overview.data.targetVersion} tone="primary" />
              <Summary label="待升级" value={`${overview.data.upgradable}`} detail={`在线 ${overview.data.online} / 离线 ${overview.data.offline}`} />
              <Summary label="已是最新" value={`${overview.data.upToDate}`} detail={`共 ${overview.data.totalNodes} 个节点`} />
              <Summary label="不可升级" value={`${overview.data.unsupported}`} detail="架构或版本不可识别" tone={overview.data.unsupported > 0 ? "warning" : "default"} />
            </div>
            {running && batch ? <InlineProgress batch={batch} /> : null}
            {!running && latest ? <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-muted/25 px-3 py-2 text-[11px] text-muted-foreground"><Clock3 className="h-3.5 w-3.5" /><span>最近任务 {latest.targetVersion}</span><span className={latest.failed > 0 ? "text-destructive" : "text-success"}>{latest.succeeded} 成功</span>{latest.failed > 0 ? <span className="text-destructive">{latest.failed} 失败</span> : null}<span className="ml-auto">{formatTime(latest.updatedAt)}</span></div> : null}
          </> : null}
        </CardContent>
      </Card>

      <Modal
        open={open}
        onClose={() => { if (!createMutation.isPending && !retryMutation.isPending) setOpen(false); }}
        title={batch ? "Agent 升级进度" : "确认升级全部 Agent"}
        description={batch ? "升级期间 Agent 会短暂断开，并以目标版本重新连接。" : "请检查目标版本和节点范围，确认后再下发升级指令。"}
        className="max-w-2xl"
        footer={batch ? (
          <>
            {batch.failed > 0 && batch.status !== "running" ? <Button variant="outline" size="sm" onClick={() => retryMutation.mutate()} disabled={retryMutation.isPending}>{retryMutation.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}重试失败节点</Button> : null}
            <Button variant="outline" size="sm" onClick={() => void batchQuery.refetch()} disabled={batchQuery.isFetching}><RefreshCw className={cn("h-3.5 w-3.5", batchQuery.isFetching && "animate-spin")} />刷新进度</Button>
            <Button size="sm" onClick={() => setOpen(false)}>收起</Button>
          </>
        ) : (
          <>
            <Button variant="outline" size="sm" onClick={() => setOpen(false)} disabled={createMutation.isPending}>取消</Button>
            <Button size="sm" onClick={() => createMutation.mutate()} disabled={createMutation.isPending || !overview.data?.upgradable}>{createMutation.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <DownloadCloud className="h-3.5 w-3.5" />}开始升级</Button>
          </>
        )}
      >
        {batch ? <BatchDetails batch={batch} /> : overview.data ? (
          <div className="space-y-4">
            <div className="rounded-xl border bg-gradient-to-br from-primary/[0.07] to-transparent p-4">
              <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-primary">Target release</p>
              <div className="mt-1 flex flex-wrap items-end gap-x-3 gap-y-1"><span className="font-mono text-xl font-semibold">{overview.data.targetVersion}</span><span className="pb-0.5 text-xs text-muted-foreground">立即升级 {overview.data.online} 个 · 离线排队 {overview.data.offline} 个</span></div>
              <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-[11px] text-muted-foreground"><span className="min-w-0 break-all font-mono">{overview.data.repository}</span><span>·</span><a href={overview.data.releaseUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-medium text-primary hover:underline">Release {overview.data.releaseTag} <ExternalLink className="h-3 w-3" /></a></div>
            </div>
            <div className="grid gap-2 sm:grid-cols-3">
              <Summary label="本次升级" value={`${overview.data.upgradable}`} detail="已识别为旧版本" />
              <Summary label="保持最新" value={`${overview.data.upToDate}`} detail="不会重复升级" />
              <Summary label="跳过" value={`${overview.data.unsupported}`} detail="版本或架构不可用" tone={overview.data.unsupported > 0 ? "warning" : "default"} />
            </div>
            <label className="flex cursor-pointer items-start gap-3 rounded-lg border bg-muted/20 p-3 text-xs">
              <input type="checkbox" checked={includeOffline} onChange={(event) => setIncludeOffline(event.target.checked)} className="mt-0.5 h-4 w-4 rounded border-input accent-primary" />
              <span><span className="font-medium text-foreground">离线节点重新上线后继续升级</span><span className="mt-1 block text-[11px] leading-5 text-muted-foreground">Server 会保留升级操作，节点重新建立 gRPC 连接后自动下发。取消勾选则本次只升级在线节点。</span></span>
            </label>
            <div className="rounded-lg border border-warning/25 bg-warning/[0.06] p-3 text-[11px] leading-5 text-warning"><div className="flex items-center gap-2 font-medium"><AlertTriangle className="h-3.5 w-3.5" />升级过程中请勿重复提交</div><p className="mt-1 text-warning/85">Agent 会在校验安装包后原子替换自身二进制并由 systemd 重启。升级完成以 Agent 使用目标版本重新连接为准。</p></div>
            {createMutation.isError ? <div role="alert" className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-xs text-destructive">{createMutation.error.message}</div> : null}
          </div>
        ) : null}
        {batch && retryMutation.isError ? <div role="alert" className="mt-3 rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-xs text-destructive">{retryMutation.error.message}</div> : null}
      </Modal>
    </>
  );
}

function BatchDetails({ batch }: { batch: AgentUpgradeBatch }) {
  const completion = batch.total > 0 ? Math.round(((batch.succeeded + batch.failed) / batch.total) * 100) : 0;
  return (
    <div className="space-y-4">
      <div className="rounded-xl border bg-muted/20 p-3.5">
        <div className="flex items-center justify-between gap-3 text-xs"><span className="font-medium">目标版本 <span className="font-mono text-primary">{batch.targetVersion}</span></span><span className="text-muted-foreground">{batch.succeeded + batch.failed} / {batch.total}</span></div>
        <Progress value={completion} className="mt-2.5 h-2" />
        <div className="mt-3 grid grid-cols-2 gap-2 text-center sm:grid-cols-4">
          <BatchCount label="排队" value={batch.queued} tone="muted" />
          <BatchCount label="执行中" value={batch.delivered} tone="warning" />
          <BatchCount label="成功" value={batch.succeeded} tone="success" />
          <BatchCount label="失败" value={batch.failed} tone="destructive" />
        </div>
      </div>
      {batch.status !== "running" ? <div className={cn("flex items-center gap-2 rounded-lg border px-3 py-2 text-xs", batch.failed > 0 ? "border-warning/25 bg-warning/[0.06] text-warning" : "border-success/20 bg-success/[0.05] text-success")}>{batch.failed > 0 ? <AlertTriangle className="h-4 w-4" /> : <CheckCircle2 className="h-4 w-4" />}{batch.failed > 0 ? "升级已结束，部分节点失败，可重试失败节点。" : "全部节点已升级完成。"}</div> : null}
      <div className="overflow-hidden rounded-lg border">
        <div className="max-h-[min(48dvh,420px)] divide-y overflow-y-auto overscroll-contain">
          {batch.items.map((item) => <UpgradeItemRow key={item.id} item={item} />)}
        </div>
      </div>
      <p className="text-[10px] leading-5 text-muted-foreground">提交时间 {formatTime(batch.createdAt)} · 最近更新 {formatTime(batch.updatedAt)}</p>
    </div>
  );
}

function UpgradeItemRow({ item }: { item: AgentUpgradeItem }) {
  const icon = item.status === "succeeded" ? <CheckCircle2 className="h-4 w-4 text-success" /> : item.status === "failed" ? <XCircle className="h-4 w-4 text-destructive" /> : item.status === "installing" || item.status === "restarting" ? <LoaderCircle className="h-4 w-4 animate-spin text-warning" /> : <Clock3 className="h-4 w-4 text-muted-foreground" />;
  return (
    <div className="flex items-start gap-3 px-3 py-2.5 text-xs">
      <div className="mt-0.5 shrink-0">{icon}</div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2"><span className="truncate font-medium">{item.nodeName}</span><Badge variant={itemStatusTone[item.status]}>{itemStatusText[item.status]}</Badge><span className="font-mono text-[10px] text-muted-foreground">{item.arch}</span></div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-muted-foreground"><span className="font-mono">{item.fromVersion} → {item.toVersion}</span>{item.message ? <span className="min-w-0 break-words">· {item.message}</span> : null}</div>
      </div>
    </div>
  );
}

function InlineProgress({ batch }: { batch: AgentUpgradeBatch }) {
  const completion = batch.total > 0 ? Math.round(((batch.succeeded + batch.failed) / batch.total) * 100) : 0;
  return <div className="rounded-lg border border-primary/20 bg-primary/[0.04] px-3 py-2.5"><div className="flex items-center justify-between text-[11px]"><span className="font-medium">升级进行中</span><span className="text-muted-foreground">{batch.succeeded + batch.failed} / {batch.total}</span></div><Progress value={completion} className="mt-2" /><div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-muted-foreground"><span className="text-success">{batch.succeeded} 成功</span><span className="text-destructive">{batch.failed} 失败</span><span>{batch.queued} 排队</span><span>{batch.delivered} 执行中</span></div></div>;
}

function Summary({ label, value, detail, tone = "default" }: { label: string; value: string; detail?: string; tone?: "default" | "primary" | "warning" }) {
  return <div className={cn("min-w-0 rounded-lg border bg-background/60 p-3", tone === "primary" && "border-primary/20 bg-primary/[0.04]", tone === "warning" && "border-warning/25 bg-warning/[0.04]")}><p className="text-[10px] text-muted-foreground">{label}</p><p className={cn("mt-1 truncate font-mono text-base font-semibold", tone === "primary" && "text-primary", tone === "warning" && "text-warning")}>{value}</p>{detail ? <p className="mt-0.5 text-[9px] leading-4 text-muted-foreground">{detail}</p> : null}</div>;
}

function BatchCount({ label, value, tone }: { label: string; value: number; tone: "muted" | "warning" | "success" | "destructive" }) {
  return <div className="rounded-md bg-background/70 py-1.5"><p className={cn("text-sm font-semibold", tone === "warning" && "text-warning", tone === "success" && "text-success", tone === "destructive" && "text-destructive")}>{value}</p><p className="text-[9px] text-muted-foreground">{label}</p></div>;
}
