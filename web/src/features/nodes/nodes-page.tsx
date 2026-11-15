import { useDeferredValue, useEffect, useState } from "react";
import { Link, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, AlertTriangle, CheckCircle2, Download, LoaderCircle, RefreshCw, Search, Server, ShieldAlert, WifiOff, X } from "lucide-react";
import type { Node, NodeGroup } from "@/api/types";
import { api } from "@/api/client";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { NodeTable } from "@/components/data/node-table";
import { PageHeader } from "@/components/layout/page-header";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Modal } from "@/components/ui/modal";
import { Segmented } from "@/components/ui/segmented";
import { cn } from "@/lib/utils";

type StatusFilter = "all" | "online" | "warning" | "offline";

export function NodesPage() {
  const search = useSearch({ strict: false }) as { q?: string };
  const [query, setQuery] = useState(search.q ?? "");
  const deferredQuery = useDeferredValue(query);
  const [status, setStatus] = useState<StatusFilter>("all");
  const [editingNode, setEditingNode] = useState<Node | null>(null);
  const [uninstallingNode, setUninstallingNode] = useState<Node | null>(null);
  const [notice, setNotice] = useState<{ tone: "success" | "warning" | "error"; message: string } | null>(null);
  const queryClient = useQueryClient();
  const nodes = useQuery({ queryKey: ["nodes", deferredQuery, status], queryFn: () => api.nodes(deferredQuery, status), refetchInterval: 15_000 });
  const groups = useQuery({ queryKey: ["groups"], queryFn: api.groups, refetchInterval: 30_000 });
  const updateNode = useMutation({
    mutationFn: ({ id, hostname, labels, groupId }: { id: string; hostname: string; labels: Record<string, string>; groupId: string }) => api.updateNode(id, { hostname, labels, groupId }),
    onSuccess: (node) => {
      setEditingNode(null);
      setNotice({ tone: "success", message: `节点「${node.hostname}」配置已更新。` });
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["groups"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
      void queryClient.invalidateQueries({ queryKey: ["overview"] });
      void queryClient.invalidateQueries({ queryKey: ["public-nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["node", node.id] });
    },
  });
  const uninstallNode = useMutation({
    mutationFn: (node: Node) => api.uninstallNode(node.id),
    onSuccess: (result) => {
      setUninstallingNode(null);
      setNotice({
        tone: result.metricsDeleted ? "success" : "warning",
        message: `已从 Beat 删除「${result.hostname}」，卸载任务 ${result.task.id.slice(0, 8)} 已排队；在线 Agent 将通过 gRPC 双向流立即执行本地清理。`,
      });
      queryClient.removeQueries({ queryKey: ["node", result.nodeId] });
      queryClient.removeQueries({ queryKey: ["node-metrics", result.nodeId] });
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["groups"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
      void queryClient.invalidateQueries({ queryKey: ["overview"] });
      void queryClient.invalidateQueries({ queryKey: ["public-nodes"] });
    },
  });

  useEffect(() => {
    if (search.q !== undefined) setQuery(search.q);
  }, [search.q]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 8_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  function openEditor(node: Node) {
    updateNode.reset();
    setEditingNode(node);
  }

  function openUninstall(node: Node) {
    uninstallNode.reset();
    setUninstallingNode(node);
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Node management"
        title="节点管理"
        description="统一查看已接入节点的连接状态、资源水位、标签与最后上报时间。"
        actions={<>
          <Button variant="outline" size="sm" onClick={() => void nodes.refetch()} disabled={nodes.isFetching}><RefreshCw className={cn("h-3.5 w-3.5", nodes.isFetching && "animate-spin")} />刷新</Button>
          <Link to="/admin/agents" className={buttonVariants({ size: "sm" })}><Download className="h-3.5 w-3.5" />接入节点</Link>
        </>}
      />

      {notice ? (
        <div className={cn("flex items-start gap-3 rounded-xl border px-4 py-3 text-xs", {
          "border-success/20 bg-success/[0.06] text-success": notice.tone === "success",
          "border-warning/25 bg-warning/[0.06] text-warning": notice.tone === "warning",
          "border-destructive/20 bg-destructive/[0.06] text-destructive": notice.tone === "error",
        })}>
          {notice.tone === "success" ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" /> : <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />}
          <p className="flex-1 leading-5">{notice.message}</p>
          <button type="button" aria-label="关闭提示" onClick={() => setNotice(null)} className="rounded p-0.5 opacity-70 hover:opacity-100"><X className="h-3.5 w-3.5" /></button>
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Summary label="全部节点" value={nodes.data?.counts.all ?? 0} icon={Server} tone="primary" />
        <Summary label="在线" value={nodes.data?.counts.online ?? 0} icon={Activity} tone="success" />
        <Summary label="高负载" value={nodes.data?.counts.warning ?? 0} icon={ShieldAlert} tone="warning" />
        <Summary label="离线" value={nodes.data?.counts.offline ?? 0} icon={WifiOff} tone="danger" />
      </div>

      <Card>
        <CardHeader className="gap-4 pb-3">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div><CardTitle>节点清单</CardTitle><p className="mt-1 text-xs text-muted-foreground">状态由最近上报时间和资源使用率共同计算。</p></div>
            <Segmented value={status} onChange={setStatus} options={[{ value: "all", label: "全部" }, { value: "online", label: "在线" }, { value: "warning", label: "高负载" }, { value: "offline", label: "离线" }]} />
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <div className="relative flex-1 sm:max-w-sm">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索主机名、IP 或操作系统" className="pl-9 pr-9" />
              {query ? <button type="button" aria-label="清空搜索" onClick={() => setQuery("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"><X className="h-3.5 w-3.5" /></button> : null}
            </div>
            <p className="self-center text-[11px] text-muted-foreground">每 15 秒自动刷新</p>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          {nodes.isPending ? <LoadingState /> : nodes.isError ? <ErrorState message={nodes.error.message} onRetry={() => void nodes.refetch()} /> : nodes.data.items.length === 0 ? (
            <EmptyState title="没有匹配的节点" description="调整搜索条件或状态筛选后重试。" />
          ) : <NodeTable nodes={nodes.data.items} admin onEdit={openEditor} onUninstall={openUninstall} />}
        </CardContent>
      </Card>

      <EditNodeDialog
        node={editingNode}
        groups={groups.data?.items ?? []}
        pending={updateNode.isPending}
        error={updateNode.error?.message}
        onClose={() => setEditingNode(null)}
        onSubmit={(hostname, labels, groupId) => {
          if (!editingNode) return;
          updateNode.mutate({ id: editingNode.id, hostname, labels, groupId });
        }}
      />
      <UninstallNodeDialog
        node={uninstallingNode}
        pending={uninstallNode.isPending}
        error={uninstallNode.error?.message}
        onClose={() => setUninstallingNode(null)}
        onConfirm={() => {
          if (uninstallingNode) uninstallNode.mutate(uninstallingNode);
        }}
      />
    </div>
  );
}

function EditNodeDialog({
  node,
  groups,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  node: Node | null;
  groups: NodeGroup[];
  pending: boolean;
  error?: string;
  onClose: () => void;
  onSubmit: (hostname: string, labels: Record<string, string>, groupId: string) => void;
}) {
  const [hostname, setHostname] = useState("");
  const [labelsText, setLabelsText] = useState("");
  const [groupId, setGroupId] = useState("");
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (!node) return;
    setHostname(node.hostname);
    setLabelsText(Object.entries(node.labels).sort(([left], [right]) => left.localeCompare(right)).map(([key, value]) => `${key}=${value}`).join(", "));
    setGroupId(node.groupId);
    setFormError("");
  }, [node]);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!hostname.trim()) {
      setFormError("节点名称不能为空");
      return;
    }
    try {
      setFormError("");
      onSubmit(hostname.trim(), parseLabelsInput(labelsText), groupId);
    } catch (parseError) {
      setFormError(parseError instanceof Error ? parseError.message : "标签格式无效");
    }
  }

  return (
    <Modal
      open={Boolean(node)}
      onClose={() => { if (!pending) onClose(); }}
      title="编辑节点配置"
      description="配置保存在 Beat 管理侧，节点持续上报时仍保持生效。"
      footer={<><button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button><button type="submit" form="edit-node-form" disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}保存配置</button></>}
    >
      <form id="edit-node-form" onSubmit={submit} className="space-y-4">
        <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">节点名称<Input autoFocus value={hostname} onChange={(event) => setHostname(event.target.value)} maxLength={128} placeholder="edge-node-01" /></label>
        <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">节点分组
          <select value={groupId} onChange={(event) => setGroupId(event.target.value)} className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-sm outline-none transition-colors focus:border-primary/40 focus:ring-2 focus:ring-primary/10">
            <option value="">未分组</option>
            {groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
          </select>
        </label>
        <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">节点标签<Input value={labelsText} onChange={(event) => setLabelsText(event.target.value)} placeholder="region=cn-east, role=edge" /><span className="block text-[10px] font-normal leading-5">多个标签使用逗号分隔，格式为 <code className="font-mono text-foreground">key=value</code>。</span></label>
        <div className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-[11px] sm:grid-cols-2">
          <div><span className="text-muted-foreground">Agent ID</span><p className="mt-0.5 truncate font-mono">{node?.agentId}</p></div>
          <div><span className="text-muted-foreground">节点地址</span><p className="mt-0.5 font-mono">{node?.address || "未上报"}</p></div>
          <div className="sm:col-span-2 text-[10px] leading-5 text-muted-foreground">采集间隔由 Agent 启动参数管理，当前为 {node?.collectorIntervalSeconds ?? 15} 秒。</div>
        </div>
        {formError || error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{formError || error}</div> : null}
      </form>
    </Modal>
  );
}

function UninstallNodeDialog({
  node,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  node: Node | null;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      open={Boolean(node)}
      onClose={() => { if (!pending) onClose(); }}
      title="卸载 Agent"
      description="此操作会立即从 Beat 移除节点，并向 Agent 下发本地清理任务。"
      className="max-w-xl"
      footer={<><button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button><button type="button" onClick={onConfirm} disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <AlertTriangle className="h-4 w-4" />}确认卸载 Agent</button></>}
    >
      <div className="space-y-4 text-xs">
        <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] p-3 text-destructive">
          <p className="font-semibold">即将卸载「{node?.hostname}」</p>
          <p className="mt-1 leading-5 text-destructive/85">确认后将删除节点记录、历史时序指标和该 Agent 的注册凭据。在线 Agent 会通过 gRPC 双向流立即收到卸载任务；离线时任务保留，待其重新连接后执行。</p>
        </div>
        <div className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-[11px] sm:grid-cols-2">
          <div><span className="text-muted-foreground">节点地址</span><p className="mt-0.5 font-mono">{node?.address || "未上报"}</p></div>
          <div><span className="text-muted-foreground">Agent ID</span><p className="mt-0.5 truncate font-mono">{node?.agentId}</p></div>
        </div>
        <div>
          <p className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Agent 本地清理范围</p>
          <ul className="space-y-1.5 text-[11px] leading-5 text-muted-foreground">
            <li>停止并移除 beat-agent systemd 服务及 <code className="font-mono text-foreground">/etc/beat</code> 配置</li>
            <li>删除 <code className="font-mono text-foreground">/usr/local/bin/beat-agent</code></li>
            <li>删除 Agent 身份文件 <code className="font-mono text-foreground">/etc/beat/agent-id</code></li>
          </ul>
        </div>
        {error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{error}</div> : null}
      </div>
    </Modal>
  );
}

function parseLabelsInput(raw: string): Record<string, string> {
  const labels: Record<string, string> = {};
  for (const pair of raw.split(",")) {
    const value = pair.trim();
    if (!value) continue;
    const separator = value.indexOf("=");
    if (separator <= 0) throw new Error(`标签「${value}」缺少 key=value 格式`);
    const key = value.slice(0, separator).trim();
    const labelValue = value.slice(separator + 1).trim();
    if (!key) throw new Error("标签键不能为空");
    labels[key] = labelValue;
  }
  return labels;
}

function Summary({ label, value, icon: Icon, tone }: { label: string; value: number; icon: typeof Server; tone: "primary" | "success" | "warning" | "danger" }) {
  return (
    <Card>
      <CardContent className="flex items-center justify-between p-4">
        <div><p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{label}</p><p className="mt-1.5 text-2xl font-semibold tabular-nums">{value}</p></div>
        <div className={cn("grid h-10 w-10 place-items-center rounded-lg", {
          "bg-primary/10 text-primary": tone === "primary",
          "bg-success/10 text-success": tone === "success",
          "bg-warning/10 text-warning": tone === "warning",
          "bg-destructive/10 text-destructive": tone === "danger",
        })}><Icon className="h-[18px] w-[18px]" /></div>
      </CardContent>
    </Card>
  );
}

