import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  FolderPlus,
  Layers3,
  LoaderCircle,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Search,
  Server,
  Trash2,
  Users,
  X,
} from "lucide-react";
import type { Node, NodeGroup, NodeGroupInput } from "@/api/types";
import { api } from "@/api/client";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Modal } from "@/components/ui/modal";
import { GROUP_COLOR_OPTIONS, groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";

export function GroupsPage() {
  const queryClient = useQueryClient();
  const groups = useQuery({ queryKey: ["groups"], queryFn: api.groups, refetchInterval: 30_000 });
  const nodes = useQuery({ queryKey: ["nodes", "", "all"], queryFn: () => api.nodes(), refetchInterval: 15_000 });
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<NodeGroup | null>(null);
  const [memberGroup, setMemberGroup] = useState<NodeGroup | null>(null);
  const [deletingGroup, setDeletingGroup] = useState<NodeGroup | null>(null);
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: "success" | "warning" | "error"; message: string } | null>(null);

  const saveGroup = useMutation({
    mutationFn: ({ group, input }: { group: NodeGroup | null; input: NodeGroupInput }) => group ? api.updateGroup(group.id, input) : api.createGroup(input),
    onSuccess: (group, variables) => {
      setEditorOpen(false);
      setEditingGroup(null);
      setNotice({ tone: "success", message: `${variables.group ? "已更新" : "已创建"}分组「${group.name}」。` });
      invalidateGroupData(queryClient);
    },
  });
  const deleteGroup = useMutation({
    mutationFn: (group: NodeGroup) => api.deleteGroup(group.id),
    onSuccess: (result) => {
      setDeletingGroup(null);
      setNotice({ tone: "success", message: `已删除分组「${result.group.name}」，其中节点已转为未分组。` });
      invalidateGroupData(queryClient);
    },
  });
  const setMembers = useMutation({
    mutationFn: ({ group, nodeIds }: { group: NodeGroup; nodeIds: string[] }) => api.setGroupMembers(group.id, nodeIds),
    onSuccess: (group) => {
      setMemberGroup(null);
      setNotice({ tone: "success", message: `分组「${group.name}」的节点成员已更新，共 ${group.nodeCount} 个节点。` });
      invalidateGroupData(queryClient);
    },
  });

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 6000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const allNodes = nodes.data?.items ?? [];
  const groupedCount = groups.data?.items.reduce((total, group) => total + group.nodeCount, 0) ?? 0;
  const refreshing = groups.isFetching || nodes.isFetching;

  function openCreate() {
    saveGroup.reset();
    setEditingGroup(null);
    setEditorOpen(true);
  }

  function openEdit(group: NodeGroup) {
    saveGroup.reset();
    setEditingGroup(group);
    setEditorOpen(true);
  }

  function openMembers(group: NodeGroup) {
    setMembers.reset();
    setMemberGroup(group);
  }

  function openDelete(group: NodeGroup) {
    deleteGroup.reset();
    setDeletingGroup(group);
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Node groups"
        title="分组管理"
        description="将节点归入业务或地域分组，前台节点广场即可按分组快速查看旗下节点。"
        actions={<>
          <Button variant="outline" size="sm" onClick={() => { void groups.refetch(); void nodes.refetch(); }} disabled={refreshing}><RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />刷新</Button>
          <Button size="sm" onClick={openCreate}><FolderPlus className="h-3.5 w-3.5" />新建分组</Button>
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

      <div className="grid gap-3 sm:grid-cols-3">
        <SummaryTile label="分组总数" value={groups.data?.total ?? 0} suffix="个" icon={Layers3} tone="primary" />
        <SummaryTile label="已分组节点" value={groupedCount} suffix="台" icon={Users} tone="success" />
        <SummaryTile label="未分组节点" value={groups.data?.ungroupedCount ?? 0} suffix="台" icon={CircleDashed} tone="muted" />
      </div>

      {groups.isPending || nodes.isPending ? <LoadingState label="正在读取节点分组…" /> : groups.isError ? (
        <ErrorState message={groups.error.message} onRetry={() => void groups.refetch()} />
      ) : groups.data.items.length === 0 ? (
        <Card>
          <CardContent className="p-0">
            <EmptyState title="还没有节点分组" description="创建第一个分组，然后在“管理节点”中批量选择成员。" />
            <div className="flex justify-center pb-8"><Button onClick={openCreate}><FolderPlus className="h-3.5 w-3.5" />创建分组</Button></div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {groups.data.items.map((group) => {
            const palette = groupColor(group.color);
            const members = allNodes.filter((node) => node.groupId === group.id);
            return (
              <Card key={group.id} className="relative">
                <div className={cn("h-1 rounded-t-xl", palette.bar)} />
                <CardContent className="p-5">
                  <div className="flex items-start justify-between gap-4">
                    <div className="flex min-w-0 items-start gap-3">
                      <div className={cn("grid h-10 w-10 shrink-0 place-items-center rounded-xl", palette.icon)}><Layers3 className="h-[18px] w-[18px]" /></div>
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <h2 className="truncate text-[15px] font-semibold tracking-tight">{group.name}</h2>
                          <Badge variant="outline" className={cn("px-1.5 py-0 text-[9px]", palette.chip)}>{group.nodeCount} 个节点</Badge>
                        </div>
                        <p className="mt-1 line-clamp-2 text-[11px] leading-5 text-muted-foreground">{group.description || "暂未填写分组描述"}</p>
                      </div>
                    </div>
                    <div className="relative shrink-0">
                      <button type="button" aria-label={`${group.name} 操作菜单`} aria-haspopup="menu" aria-expanded={openMenuID === group.id} onClick={() => setOpenMenuID(openMenuID === group.id ? null : group.id)} className={cn("grid h-8 w-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground", openMenuID === group.id && "bg-muted text-foreground")}><MoreHorizontal className="h-4 w-4" /></button>
                      {openMenuID === group.id ? (
                        <>
                          <button type="button" aria-label="关闭操作菜单" className="fixed inset-0 z-10 cursor-default" onClick={() => setOpenMenuID(null)} />
                          <div role="menu" className="absolute right-0 top-9 z-20 w-40 overflow-hidden rounded-lg border bg-card p-1 shadow-xl">
                            <button type="button" role="menuitem" onClick={() => { setOpenMenuID(null); openEdit(group); }} className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium hover:bg-primary/10 hover:text-primary"><Pencil className="h-3.5 w-3.5" />编辑分组</button>
                            <button type="button" role="menuitem" onClick={() => { setOpenMenuID(null); openMembers(group); }} className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium hover:bg-primary/10 hover:text-primary"><Users className="h-3.5 w-3.5" />管理节点</button>
                            <div className="my-1 h-px bg-border" />
                            <button type="button" role="menuitem" onClick={() => { setOpenMenuID(null); openDelete(group); }} className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium text-destructive hover:bg-destructive/10"><Trash2 className="h-3.5 w-3.5" />删除分组</button>
                          </div>
                        </>
                      ) : null}
                    </div>
                  </div>

                  <div className="mt-4 grid grid-cols-3 gap-2 rounded-lg border bg-muted/25 p-3">
                    <GroupStat label="在线" value={group.onlineCount} tone="success" />
                    <GroupStat label="高负载" value={group.warningCount} tone="warning" />
                    <GroupStat label="离线" value={group.offlineCount} tone="muted" />
                  </div>

                  <div className="mt-4 min-h-[92px] rounded-lg border bg-background/60 p-3">
                    <div className="mb-2 flex items-center justify-between"><p className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">节点成员</p><span className="text-[10px] text-muted-foreground">显示前 4 个</span></div>
                    {members.length === 0 ? (
                      <button type="button" onClick={() => openMembers(group)} className="flex w-full items-center justify-center gap-2 rounded-md border border-dashed px-3 py-4 text-[11px] text-muted-foreground hover:border-primary/30 hover:text-primary"><FolderPlus className="h-3.5 w-3.5" />添加节点到此分组</button>
                    ) : (
                      <div className="space-y-1.5">
                        {members.slice(0, 4).map((node) => (
                          <Link key={node.id} to="/admin/nodes/$nodeId" params={{ nodeId: node.id }} className="flex items-center gap-2 rounded-md px-1.5 py-1 text-[11px] transition-colors hover:bg-muted/60">
                            <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", node.status === "online" ? "bg-success" : node.status === "warning" ? "bg-warning" : "bg-muted-foreground/50")} />
                            <span className="min-w-0 flex-1 truncate font-medium">{node.hostname}</span>
                            <span className="font-mono text-[9px] text-muted-foreground">{node.address || node.agentId.slice(0, 12)}</span>
                          </Link>
                        ))}
                        {members.length > 4 ? <p className="px-1.5 pt-1 text-[10px] text-muted-foreground">另有 {members.length - 4} 个节点</p> : null}
                      </div>
                    )}
                  </div>

                  <div className="mt-4 flex items-center justify-between gap-3">
                    <p className="text-[10px] text-muted-foreground">更新于 {formatGroupTime(group.updatedAt)}</p>
                    <Button variant="outline" size="sm" onClick={() => openMembers(group)}><Users className="h-3.5 w-3.5" />管理节点</Button>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      {groups.data ? (
        <Card className={cn("border-dashed", groups.data.ungroupedCount > 0 ? "bg-muted/20" : "bg-card")}>
          <CardContent className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-3">
              <div className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground"><CircleDashed className="h-[18px] w-[18px]" /></div>
              <div><p className="text-sm font-semibold">未分组节点</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">当前有 {groups.data.ungroupedCount} 个节点未归属任何分组，可在节点管理中编辑节点或在上方分组中批量归组。</p></div>
            </div>
            <Link to="/admin/nodes" className={buttonVariants({ variant: "outline", size: "sm" })}><Server className="h-3.5 w-3.5" />前往节点管理</Link>
          </CardContent>
        </Card>
      ) : null}

      <GroupEditorDialog
        open={editorOpen}
        group={editingGroup}
        pending={saveGroup.isPending}
        error={saveGroup.error?.message}
        onClose={() => { if (!saveGroup.isPending) { setEditorOpen(false); setEditingGroup(null); } }}
        onSubmit={(input) => saveGroup.mutate({ group: editingGroup, input })}
      />
      <GroupMembersDialog
        group={memberGroup}
        nodes={allNodes}
        pending={setMembers.isPending}
        error={setMembers.error?.message}
        onClose={() => { if (!setMembers.isPending) setMemberGroup(null); }}
        onSubmit={(nodeIds) => { if (memberGroup) setMembers.mutate({ group: memberGroup, nodeIds }); }}
      />
      <DeleteGroupDialog
        group={deletingGroup}
        pending={deleteGroup.isPending}
        error={deleteGroup.error?.message}
        onClose={() => { if (!deleteGroup.isPending) setDeletingGroup(null); }}
        onConfirm={() => { if (deletingGroup) deleteGroup.mutate(deletingGroup); }}
      />
    </div>
  );
}

function SummaryTile({ label, value, suffix, icon: Icon, tone }: { label: string; value: number; suffix: string; icon: typeof Layers3; tone: "primary" | "success" | "muted" }) {
  return (
    <Card>
      <CardContent className="flex items-center justify-between p-4">
        <div><p className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{label}</p><p className="mt-1.5 text-2xl font-semibold tabular-nums">{value}<span className="ml-1 text-xs font-medium text-muted-foreground">{suffix}</span></p></div>
        <div className={cn("grid h-10 w-10 place-items-center rounded-lg", {
          "bg-primary/10 text-primary": tone === "primary",
          "bg-success/10 text-success": tone === "success",
          "bg-muted text-muted-foreground": tone === "muted",
        })}><Icon className="h-[18px] w-[18px]" /></div>
      </CardContent>
    </Card>
  );
}

function GroupStat({ label, value, tone }: { label: string; value: number; tone: "success" | "warning" | "muted" }) {
  return <div className="text-center"><p className="text-[9px] font-medium text-muted-foreground">{label}</p><p className={cn("mt-1 text-sm font-semibold tabular-nums", {
    "text-success": tone === "success",
    "text-warning": tone === "warning",
    "text-muted-foreground": tone === "muted",
  })}>{value}</p></div>;
}

function GroupEditorDialog({ open, group, pending, error, onClose, onSubmit }: {
  open: boolean;
  group: NodeGroup | null;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onSubmit: (input: NodeGroupInput) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [color, setColor] = useState("blue");
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (!open) return;
    setName(group?.name ?? "");
    setDescription(group?.description ?? "");
    setColor(group?.color ?? "blue");
    setFormError("");
  }, [open, group]);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setFormError("分组名称不能为空");
      return;
    }
    setFormError("");
    onSubmit({ name: name.trim(), description: description.trim(), color });
  }

  return (
    <Modal open={open} onClose={onClose} title={group ? "编辑分组" : "新建分组"} description="分组用于后台归档和前台节点筛选，不会影响 Agent 采集。" footer={<>
      <button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button>
      <button type="submit" form="group-editor-form" disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{group ? "保存分组" : "创建分组"}</button>
    </>}>
      <form id="group-editor-form" onSubmit={submit} className="space-y-4">
        <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">分组名称<Input autoFocus value={name} onChange={(event) => setName(event.target.value)} maxLength={48} placeholder="例如：华东边缘接入" /></label>
        <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">分组描述<textarea value={description} onChange={(event) => setDescription(event.target.value)} maxLength={200} rows={3} placeholder="说明该分组承载的业务、地域或节点类型" className="flex w-full resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm outline-none transition-colors placeholder:text-muted-foreground focus:border-primary/40 focus:ring-2 focus:ring-primary/10" /></label>
        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground">识别色</p>
          <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
            {GROUP_COLOR_OPTIONS.map((option) => <button key={option.id} type="button" aria-pressed={color === option.id} onClick={() => setColor(option.id)} className={cn("flex items-center justify-center gap-1.5 rounded-lg border px-2 py-2 text-[10px] font-medium transition-colors", color === option.id ? "border-primary/30 bg-primary/[0.04] text-foreground" : "text-muted-foreground hover:bg-muted/50")}><span className={cn("h-2.5 w-2.5 rounded-full", option.swatch)} />{option.label}</button>)}
          </div>
        </div>
        {formError || error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{formError || error}</div> : null}
      </form>
    </Modal>
  );
}

function GroupMembersDialog({ group, nodes, pending, error, onClose, onSubmit }: {
  group: NodeGroup | null;
  nodes: Node[];
  pending: boolean;
  error?: string;
  onClose: () => void;
  onSubmit: (nodeIds: string[]) => void;
}) {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!group) return;
    setSelected(new Set(nodes.filter((node) => node.groupId === group.id).map((node) => node.id)));
    setQuery("");
  }, [group, nodes]);

  const normalizedQuery = query.trim().toLowerCase();
  const visibleNodes = nodes.filter((node) => !normalizedQuery || `${node.hostname} ${node.address} ${node.groupName}`.toLowerCase().includes(normalizedQuery));

  function toggle(nodeID: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(nodeID)) next.delete(nodeID); else next.add(nodeID);
      return next;
    });
  }

  return (
    <Modal open={Boolean(group)} onClose={onClose} title={`管理「${group?.name ?? ""}」节点`} description="勾选的节点会加入当前分组；取消勾选会移出分组。节点只能属于一个分组。" className="max-w-2xl" footer={<>
      <span className="mr-auto self-center text-[11px] text-muted-foreground">已选择 {selected.size} 个节点</span>
      <button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button>
      <button type="button" onClick={() => onSubmit([...selected])} disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}保存成员</button>
    </>}>
      <div className="space-y-3">
        <div className="relative"><Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" /><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索主机名、IP 或所属分组" className="pl-9" /></div>
        <div className="max-h-[340px] space-y-1 overflow-y-auto rounded-lg border bg-muted/15 p-1.5 scrollbar-thin">
          {visibleNodes.length === 0 ? <p className="px-3 py-8 text-center text-xs text-muted-foreground">没有匹配的节点</p> : visibleNodes.map((node) => {
            const checked = selected.has(node.id);
            const otherGroup = node.groupId && node.groupId !== group?.id ? groupColor(node.groupColor) : null;
            return (
              <label key={node.id} className={cn("flex cursor-pointer items-center gap-3 rounded-md border border-transparent px-3 py-2.5 transition-colors hover:bg-card", checked && "border-primary/15 bg-primary/[0.04]")}>
                <input type="checkbox" checked={checked} onChange={() => toggle(node.id)} className="h-4 w-4 rounded border-input accent-primary" />
                <span className={cn("h-2 w-2 rounded-full", node.status === "online" ? "bg-success" : node.status === "warning" ? "bg-warning" : "bg-muted-foreground/50")} />
                <span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{node.hostname}</span><span className="mt-0.5 block truncate font-mono text-[9px] text-muted-foreground">{node.address || node.agentId}</span></span>
                <span className={cn("shrink-0 rounded border px-1.5 py-0.5 text-[9px]", otherGroup?.chip ?? "border-dashed bg-muted/35 text-muted-foreground")}>{otherGroup ? `来自 ${node.groupName}` : node.groupId ? "当前分组" : "未分组"}</span>
              </label>
            );
          })}
        </div>
        {error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{error}</div> : null}
      </div>
    </Modal>
  );
}

function DeleteGroupDialog({ group, pending, error, onClose, onConfirm }: { group: NodeGroup | null; pending: boolean; error?: string; onClose: () => void; onConfirm: () => void }) {
  return (
    <Modal open={Boolean(group)} onClose={onClose} title="删除分组" description="分组删除后不可恢复，但不会删除节点或影响指标采集。" footer={<>
      <button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button>
      <button type="button" onClick={onConfirm} disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}确认删除</button>
    </>}>
      <div className="space-y-3 text-xs">
        <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] p-3 text-destructive"><p className="font-semibold">即将删除「{group?.name}」</p><p className="mt-1 leading-5 text-destructive/85">该分组下的 {group?.nodeCount ?? 0} 个节点会转为未分组，节点本身和历史指标不受影响。</p></div>
        {error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{error}</div> : null}
      </div>
    </Modal>
  );
}

function invalidateGroupData(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: ["groups"] });
  void queryClient.invalidateQueries({ queryKey: ["nodes"] });
  void queryClient.invalidateQueries({ queryKey: ["public-nodes"] });
  void queryClient.invalidateQueries({ queryKey: ["overview"] });
  void queryClient.invalidateQueries({ queryKey: ["node"] });
}

function formatGroupTime(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}
