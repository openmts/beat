import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  ArrowDownUp,
  CheckCircle2,
  Clock3,
  Code2,
  Database,
  Globe2,
  History,
  ListChecks,
  ListPlus,
  LoaderCircle,
  Network,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldAlert,
  Terminal,
  Timer,
  Trash2,
  Wand2,
} from "lucide-react";
import type {
  AddressFamily,
  Carrier,
  MetricExtractorType,
  NetworkProbeResult,
  NetworkProbeTargetResult,
  ShellMetricConfig,
  TaskDefinition,
  TaskInput,
  TaskKind,
  TaskMetricDefinition,
  TaskRun,
  TaskRunStatus,
  ThreeNetworkConfig,
  ThreeNetworkTarget,
} from "@/api/types";
import { api } from "@/api/client";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Modal } from "@/components/ui/modal";
import { Switch } from "@/components/ui/switch";
import { formatRelative, formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

type Notice = { tone: "success" | "warning" | "error"; message: string };

const CARRIERS: Array<{ id: Carrier; name: string; short: string; accent: string; dot: string }> = [
  { id: "telecom", name: "中国电信", short: "电信", accent: "border-cyan-500/20 bg-cyan-500/[0.035]", dot: "bg-cyan-500" },
  { id: "unicom", name: "中国联通", short: "联通", accent: "border-rose-500/20 bg-rose-500/[0.035]", dot: "bg-rose-500" },
  { id: "mobile", name: "中国移动", short: "移动", accent: "border-emerald-500/20 bg-emerald-500/[0.035]", dot: "bg-emerald-500" },
];

const DEFAULT_SCRIPT = "#!/bin/sh\nset -u\n\nprintf 'hello from beat-agent\\n'";
const DEFAULT_METRIC_MEASUREMENT = "task_metrics";
const DEFAULT_METRIC_SAMPLE = '{\n  "connections": 42,\n  "qps": 118.7,\n  "replica_lag_ms": 38,\n  "pool": { "active": 12, "idle": 4 }\n}';

function defaultNetworkConfig(): ThreeNetworkConfig {
  return {
    targets: [
      { carrier: "telecom", label: "电信目标", host: "www.189.cn", port: 443, protocol: "tcp", addressFamily: "auto" },
      { carrier: "unicom", label: "联通目标", host: "www.10010.com", port: 443, protocol: "tcp", addressFamily: "auto" },
      { carrier: "mobile", label: "移动目标", host: "www.10086.cn", port: 443, protocol: "tcp", addressFamily: "auto" },
    ],
    probe: { count: 10, intervalMs: 200, timeoutMs: 1000 },
  };
}

function emptyTask(): TaskInput {
  const config = defaultNetworkConfig();
  return {
    name: "三网质量巡检",
    description: "从 Agent 所在节点发起 TCP Connect 探测，检查三网目标的时延与丢包。",
    kind: "three_network_probe",
    script: "",
    config,
    intervalSeconds: 300,
    timeoutSeconds: 60,
    enabled: false,
  };
}

function isThreeNetworkConfig(config: unknown): config is ThreeNetworkConfig {
  return Boolean(config && typeof config === "object" && "targets" in config && Array.isArray((config as ThreeNetworkConfig).targets) && "probe" in config);
}

function isShellMetricConfig(config: unknown): config is ShellMetricConfig {
  return Boolean(config && typeof config === "object" && "metricSchema" in config);
}

function carrierMeta(carrier: Carrier) {
  return CARRIERS.find((item) => item.id === carrier) ?? CARRIERS[0];
}

export function TasksPage() {
  const queryClient = useQueryClient();
  const tasks = useQuery({ queryKey: ["tasks"], queryFn: api.tasks, refetchInterval: 15_000 });
  const [runTaskID, setRunTaskID] = useState("");
  const runs = useQuery({
    queryKey: ["task-runs", runTaskID],
    queryFn: () => api.taskRuns(runTaskID, 50),
    refetchInterval: 10_000,
  });
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<TaskDefinition | null>(null);
  const [scriptTask, setScriptTask] = useState<TaskDefinition | null>(null);
  const [deletingTask, setDeletingTask] = useState<TaskDefinition | null>(null);
  const [selectedRun, setSelectedRun] = useState<TaskRun | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const runDetail = useQuery({
    queryKey: ["task-run", selectedRun?.id],
    queryFn: () => api.taskRun(selectedRun!.id),
    enabled: Boolean(selectedRun),
  });

  const saveTask = useMutation({
    mutationFn: ({ task, input }: { task: TaskDefinition | null; input: TaskInput }) => task ? api.updateTask(task.id, input) : api.createTask(input),
    onSuccess: (task, variables) => {
      setEditorOpen(false);
      setEditingTask(null);
      const action = variables.task ? "已更新任务" : "已创建任务";
      setNotice({ tone: "success", message: `${action}「${task.name}」。${task.enabled ? "已开启，将在下一个调度窗口通过 gRPC 下发。" : "当前保持停用。"}` });
      void invalidateTasks(queryClient);
    },
  });
  const toggleTask = useMutation({
    mutationFn: ({ task, enabled }: { task: TaskDefinition; enabled: boolean }) => api.updateTask(task.id, { enabled }),
    onSuccess: (task) => {
      setNotice({ tone: "success", message: `任务「${task.name}」已${task.enabled ? "启用" : "停用"}。${task.enabled ? "等待下一个调度窗口通过 gRPC 下发。" : ""}` });
      void invalidateTasks(queryClient);
    },
    onError: (error) => setNotice({ tone: "error", message: error.message }),
  });
  const deleteTask = useMutation({
    mutationFn: (task: TaskDefinition) => api.deleteTask(task.id),
    onSuccess: (_, task) => {
      setDeletingTask(null);
      setNotice({ tone: "success", message: `已删除任务「${task.name}」，历史执行记录仍保留。` });
      void invalidateTasks(queryClient);
    },
    onError: (error) => setNotice({ tone: "error", message: error.message }),
  });

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 7_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  if (tasks.isPending) return <LoadingState label="正在读取任务中心…" />;
  if (tasks.isError) return <ErrorState message={tasks.error.message} onRetry={() => void tasks.refetch()} />;

  const summary = tasks.data.summary;
  const refreshing = tasks.isFetching || runs.isFetching;

  function openCreate() {
    saveTask.reset();
    setEditingTask(null);
    setEditorOpen(true);
  }

  function openEdit(task: TaskDefinition) {
    saveTask.reset();
    setEditingTask(task);
    setEditorOpen(true);
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Automation center"
        title="任务中心"
        description="由 Server 统一调度巡检脚本，通过 gRPC 双向流即时下发，Agent 并行执行并将结果回传。任务由管理员按需创建和维护。"
        actions={<>
          <Button variant="outline" size="sm" onClick={() => { void tasks.refetch(); void runs.refetch(); }} disabled={refreshing}><RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />刷新</Button>
          <Button size="sm" onClick={openCreate}><Plus className="h-3.5 w-3.5" />新建任务</Button>
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
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard icon={ListChecks} label="已启用任务" value={String(summary.enabled)} hint={`共 ${summary.total} 个任务定义`} tone="primary" />
        <SummaryCard icon={Code2} label="任务定义" value={String(summary.total)} hint="由管理员创建和维护" />
        <SummaryCard icon={History} label="累计执行" value={String(summary.runs)} hint="Agent 已接收的任务运行数" />
        <SummaryCard icon={AlertTriangle} label="失败与超时" value={String(summary.failures)} hint="需要关注的执行结果" tone={summary.failures > 0 ? "warning" : "success"} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(360px,0.9fr)]">
        <Card className="overflow-hidden">
          <CardHeader className="border-b bg-gradient-to-r from-card to-primary/[0.025]">
            <div>
              <CardTitle className="flex items-center gap-2"><Terminal className="h-4 w-4 text-primary" />任务定义</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">启用后，Server 在每个周期窗口通过 gRPC 向每个 Agent 推送一次任务。</p>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 p-3 sm:p-4">
            {(tasks.data.items.length === 0) ? <EmptyTaskTemplate onCreate={openCreate} /> : tasks.data.items.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                toggling={toggleTask.isPending && toggleTask.variables?.task.id === task.id}
                onToggle={() => toggleTask.mutate({ task, enabled: !task.enabled })}
                onShowScript={() => setScriptTask(task)}
                onEdit={() => openEdit(task)}
                onDelete={() => { deleteTask.reset(); setDeletingTask(task); }}
                onShowRuns={() => setRunTaskID(task.id)}
              />
            ))}
          </CardContent>
        </Card>

        <Card className="overflow-hidden xl:sticky xl:top-20 xl:self-start">
          <CardHeader className="border-b">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="flex items-center gap-2"><History className="h-4 w-4 text-primary" />最近执行</CardTitle>
                <p className="mt-1 text-xs text-muted-foreground">结果由 Agent 在脚本结束后主动回传。</p>
              </div>
              <Badge variant="outline">{runs.data?.total ?? 0} 条</Badge>
            </div>
            <select value={runTaskID} onChange={(event) => setRunTaskID(event.target.value)} className="mt-3 h-9 w-full rounded-md border border-input bg-card px-3 text-xs shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/30">
              <option value="">全部任务</option>
              {tasks.data.items.map((task) => <option key={task.id} value={task.id}>{task.name}</option>)}
            </select>
          </CardHeader>
          <CardContent className="p-2.5">
            {runs.isPending ? <LoadingState label="正在读取执行记录…" /> : null}
            {runs.isError ? <ErrorState message={runs.error.message} onRetry={() => void runs.refetch()} /> : null}
            {!runs.isPending && !runs.isError && (runs.data?.items.length ?? 0) === 0 ? <EmptyState title="暂无执行记录" description="启用任务后，等待下一个调度窗口。" /> : null}
            <div className="max-h-[660px] space-y-2 overflow-y-auto scrollbar-thin">
              {runs.data?.items.map((run) => <RunRow key={run.id} run={run} onClick={() => setSelectedRun(run)} />)}
            </div>
          </CardContent>
        </Card>
      </div>

      <TaskEditorDialog open={editorOpen} task={editingTask} pending={saveTask.isPending} error={saveTask.error?.message} onClose={() => setEditorOpen(false)} onSubmit={(input) => saveTask.mutate({ task: editingTask, input })} />
      <ScriptDialog task={scriptTask} onClose={() => setScriptTask(null)} />
      <RunDetailDialog run={selectedRun} detail={runDetail.data} loading={runDetail.isPending} error={runDetail.error?.message} onClose={() => setSelectedRun(null)} />
      <DeleteTaskDialog task={deletingTask} pending={deleteTask.isPending} error={deleteTask.error?.message} onClose={() => setDeletingTask(null)} onConfirm={() => deletingTask && deleteTask.mutate(deletingTask)} />
    </div>
  );
}

function SummaryCard({ icon: Icon, label, value, hint, tone = "default" }: { icon: typeof ListChecks; label: string; value: string; hint: string; tone?: "default" | "primary" | "success" | "warning" }) {
  return (
    <Card className="overflow-hidden"><CardContent className="flex items-center gap-3 p-4">
      <div className={cn("grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground", tone === "primary" && "bg-primary/10 text-primary", tone === "success" && "bg-success/10 text-success", tone === "warning" && "bg-warning/10 text-warning")}><Icon className="h-4 w-4" /></div>
      <div className="min-w-0"><p className="text-[10px] font-medium text-muted-foreground">{label}</p><p className="mt-0.5 text-xl font-semibold tracking-tight tabular-nums">{value}</p><p className="mt-0.5 truncate text-[9px] text-muted-foreground">{hint}</p></div>
    </CardContent></Card>
  );
}

function EmptyTaskTemplate({ onCreate }: { onCreate: () => void }) {
  const config = defaultNetworkConfig();
  return (
    <div className="overflow-hidden rounded-2xl border border-primary/15 bg-gradient-to-br from-primary/[0.065] via-card to-card">
      <div className="p-4 sm:p-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex items-start gap-3">
            <div className="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm"><Network className="h-5 w-5" /></div>
            <div><div className="flex flex-wrap items-center gap-2"><p className="text-sm font-semibold">三网时延与丢包检测</p><Badge variant="default">推荐模板</Badge></div><p className="mt-1 max-w-xl text-[11px] leading-5 text-muted-foreground">按电信、联通、移动分别配置 TCP 目标，采集建连时延、丢包/失败率、P95 和抖动。模板不会被预先创建，只有确认配置后才会下发。</p></div>
          </div>
          <Button size="sm" onClick={onCreate}><Plus className="h-3.5 w-3.5" />使用三网模板</Button>
        </div>
        <div className="mt-4 grid gap-2 sm:grid-cols-3">
          {config.targets.map((target) => {
            const meta = carrierMeta(target.carrier);
            return <div key={target.carrier} className={cn("rounded-xl border bg-card/70 p-3", meta.accent)}>
              <div className="flex items-center gap-2"><span className={cn("h-2 w-2 rounded-full", meta.dot)} /><span className="text-[10px] font-semibold">{meta.name}</span><Badge variant="outline" className="ml-auto px-1.5 py-0 text-[8px]">TCP</Badge></div>
              <p className="mt-2 truncate font-mono text-[10px] text-foreground/85">{target.host}:{target.port}</p>
              <p className="mt-1 text-[9px] text-muted-foreground">可替换为实际检查节点</p>
            </div>;
          })}
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[9px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5"><Activity className="h-3 w-3 text-primary" />TCP Connect 时延</span>
          <span className="inline-flex items-center gap-1.5"><ArrowDownUp className="h-3 w-3 text-primary" />丢包 / 失败率</span>
          <span className="inline-flex items-center gap-1.5"><Globe2 className="h-3 w-3 text-primary" />IPv4 / IPv6 / 自动</span>
          <span className="inline-flex items-center gap-1.5"><Timer className="h-3 w-3 text-primary" />默认每 5 分钟执行</span>
        </div>
      </div>
    </div>
  );
}

function TaskCard({ task, toggling, onToggle, onShowScript, onEdit, onDelete, onShowRuns }: {
  task: TaskDefinition; toggling: boolean; onToggle: () => void; onShowScript: () => void; onEdit: () => void; onDelete: () => void; onShowRuns: () => void;
}) {
  const networkConfig = task.kind === "three_network_probe" && isThreeNetworkConfig(task.config) ? task.config : null;
  const shellConfig = task.kind === "shell_script" && isShellMetricConfig(task.config) ? task.config.metricSchema : null;
  const metricMetrics = shellConfig?.enabled ? shellConfig.metrics : [];
  const network = Boolean(networkConfig);
  return (
    <div className={cn("rounded-xl border p-3.5 transition-colors sm:p-4", task.enabled ? "border-primary/20 bg-primary/[0.018]" : "bg-background/45")}>
      <div className="flex items-start gap-3">
        <div className={cn("grid h-9 w-9 shrink-0 place-items-center rounded-lg", network ? "bg-primary/10 text-primary" : "bg-violet-500/10 text-violet-600 dark:text-violet-300")}>{network ? <Network className="h-4 w-4" /> : <Code2 className="h-4 w-4" />}</div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2"><p className="text-sm font-semibold tracking-tight">{task.name}</p>{network ? <Badge variant="default">三网 TCP</Badge> : <Badge variant="outline">Shell</Badge>}{metricMetrics.length > 0 ? <Badge variant="default">{metricMetrics.length} 项指标</Badge> : null}{task.enabled ? <Badge variant="success">已启用</Badge> : <Badge variant="outline">未启用</Badge>}</div>
          <p className="mt-1.5 text-[11px] leading-5 text-muted-foreground">{task.description || "未填写任务说明"}</p>
          {networkConfig ? <div className="mt-2.5 flex flex-wrap gap-1.5">{networkConfig.targets.map((target) => {
            const meta = carrierMeta(target.carrier);
            return <span key={target.carrier} className="inline-flex items-center gap-1.5 rounded-md border bg-card/70 px-2 py-1 text-[9px] text-muted-foreground"><span className={cn("h-1.5 w-1.5 rounded-full", meta.dot)} />{meta.short} · <span className="font-mono text-foreground/85">{target.host}:{target.port}</span></span>;
          })}</div> : null}
          {metricMetrics.length > 0 ? <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
            <span className="inline-flex items-center gap-1.5 rounded-md border border-primary/20 bg-primary/[0.045] px-2 py-1 text-[9px] font-medium text-primary"><Database className="h-2.5 w-2.5" />MTS · <span className="font-mono">{shellConfig?.measurement || DEFAULT_METRIC_MEASUREMENT}</span></span>
            {metricMetrics.slice(0, 6).map((metric) => <span key={metric.key} className="rounded-md border bg-card/70 px-2 py-1 font-mono text-[9px] text-muted-foreground">{metric.key}</span>)}
            {metricMetrics.length > 6 ? <span className="text-[9px] text-muted-foreground">+{metricMetrics.length - 6}</span> : null}
          </div> : null}
        </div>
        <Switch checked={task.enabled} onCheckedChange={onToggle} loading={toggling} label={`${task.enabled ? "停用" : "启用"}${task.name}`} />
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <TaskMeta label="执行周期" value={formatInterval(task.intervalSeconds)} />
        <TaskMeta label={networkConfig ? "TCP 探测" : shellConfig?.enabled ? "指标采集" : "执行超时"} value={networkConfig ? `${networkConfig.probe.count} 次 / ${networkConfig.probe.intervalMs}ms` : shellConfig?.enabled ? `${shellConfig.metrics.length} 个字段` : `${task.timeoutSeconds}s`} />
        <TaskMeta label="最近状态" value={task.lastStatus ? runStatusLabel(task.lastStatus) : "未执行"} />
        <TaskMeta label="成功 / 失败" value={`${task.successCount} / ${task.failureCount}`} />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 border-t pt-3">
        <span className="mr-auto flex min-w-0 items-center gap-1.5 text-[9px] text-muted-foreground"><Clock3 className="h-3 w-3 shrink-0" />{task.lastRunAt ? `最近执行 ${formatRelative(task.lastRunAt)}` : "启用后等待 Agent 上报"}</span>
        {!network ? <Button type="button" variant="ghost" size="sm" onClick={onShowScript}><Terminal className="h-3.5 w-3.5" />脚本</Button> : null}
        <Button type="button" variant="ghost" size="sm" onClick={onShowRuns}><History className="h-3.5 w-3.5" />记录</Button>
        <Button type="button" variant="ghost" size="sm" onClick={onEdit}><Pencil className="h-3.5 w-3.5" />{network ? "配置" : "编辑"}</Button>
        <Button type="button" variant="ghost" size="sm" className="text-destructive hover:bg-destructive/10 hover:text-destructive" onClick={onDelete}><Trash2 className="h-3.5 w-3.5" />删除</Button>
      </div>
    </div>
  );
}

function TaskMeta({ label, value }: { label: string; value: string }) {
  return <div className="rounded-lg bg-muted/40 px-2.5 py-2"><p className="text-[8px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">{label}</p><p className="mt-1 truncate text-[10px] font-semibold">{value}</p></div>;
}

function RunRow({ run, onClick }: { run: TaskRun; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="w-full rounded-lg border bg-background/45 p-3 text-left transition-colors hover:border-primary/20 hover:bg-primary/[0.025]">
      <div className="flex items-start gap-2.5">
        <RunStatusIcon status={run.status} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2"><p className="truncate text-[11px] font-semibold">{run.taskName}</p><RunStatusBadge status={run.status} /></div>
          <p className="mt-1 flex items-center gap-1.5 truncate text-[9px] text-muted-foreground"><Server className="h-2.5 w-2.5" />{run.nodeName || run.nodeId || "未知节点"} · {run.durationMs > 0 ? formatDuration(run.durationMs) : formatRelative(run.scheduledAt)}</p>
          <p className="mt-1 line-clamp-2 whitespace-pre-wrap break-words font-mono text-[9px] leading-4 text-muted-foreground/85">{runOutputPreview(run)}</p>
        </div>
        <span className="shrink-0 text-[8px] text-muted-foreground">{formatRelative(run.completedAt || run.scheduledAt)}</span>
      </div>
    </button>
  );
}

function RunStatusIcon({ status }: { status: TaskRunStatus }) {
  const className = "mt-0.5 h-3.5 w-3.5 shrink-0";
  if (status === "success") return <CheckCircle2 className={cn(className, "text-success")} />;
  if (status === "failed") return <AlertTriangle className={cn(className, "text-destructive")} />;
  if (status === "timeout") return <Timer className={cn(className, "text-warning")} />;
  return <Clock3 className={cn(className, "text-primary")} />;
}

function RunStatusBadge({ status }: { status: TaskRunStatus }) {
  const variant = status === "success" ? "success" : status === "failed" ? "destructive" : status === "timeout" ? "warning" : "default";
  return <Badge variant={variant} className="shrink-0 px-1.5 py-0 text-[8px]">{runStatusLabel(status)}</Badge>;
}

type MetricCandidate = {
  key: string;
  expression: string;
  type: MetricExtractorType;
  value: number;
};

function emptyMetric(): TaskMetricDefinition {
  return { key: "", name: "", extractor: { type: "json_path", expression: "" }, unit: "" };
}

function metricKeyFromPath(path: string) {
  const value = path.replace(/^\$\.?/, "").replace(/\[([0-9]+)\]/g, "_$1").replace(/[^a-zA-Z0-9]+/g, "_").replace(/^_+|_+$/g, "").toLowerCase();
  const safe = value.replace(/^[0-9]+/, "metric_$&").slice(0, 64);
  return safe || "metric";
}

function suggestedMetricUnit(key: string) {
  if (key.endsWith("_ms")) return "ms";
  if (key.endsWith("_bytes")) return "B";
  if (key.endsWith("_percent") || key.endsWith("_pct")) return "%";
  if (key.endsWith("_seconds") || key.endsWith("_sec")) return "s";
  return "";
}

function collectJSONMetricCandidates(value: unknown, path: string, candidates: MetricCandidate[]) {
  if (typeof value === "number" && Number.isFinite(value)) {
    const key = metricKeyFromPath(path);
    candidates.push({ key, expression: path, type: "json_path", value });
    return;
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  for (const [childKey, childValue] of Object.entries(value)) {
    collectJSONMetricCandidates(childValue, `${path}.${childKey}`, candidates);
  }
}

function recognizeMetricCandidates(output: string): MetricCandidate[] {
  const text = output.trim();
  if (!text) return [];
  try {
    const parsed = JSON.parse(text) as unknown;
    const candidates: MetricCandidate[] = [];
    collectJSONMetricCandidates(parsed, "$", candidates);
    if (candidates.length > 0) return candidates;
  } catch {
    // Fall through to key=value parsing for plain-text command output.
  }
  const candidates: MetricCandidate[] = [];
  const pattern = /^\s*([a-zA-Z_][a-zA-Z0-9_.-]*)\s*[=:]\s*(-?\d+(?:\.\d+)?)\s*$/gm;
  for (const match of text.matchAll(pattern)) {
    const key = metricKeyFromPath(match[1]);
    candidates.push({ key, expression: match[1], type: "key_value", value: Number(match[2]) });
  }
  return candidates;
}

function MetricOrchestration({ enabled, measurement, metrics, onEnabledChange, onMeasurementChange, onMetricsChange }: {
  enabled: boolean;
  measurement: string;
  metrics: TaskMetricDefinition[];
  onEnabledChange: (enabled: boolean) => void;
  onMeasurementChange: (measurement: string) => void;
  onMetricsChange: (metrics: TaskMetricDefinition[]) => void;
}) {
  const [sampleOpen, setSampleOpen] = useState(false);
  const [sampleOutput, setSampleOutput] = useState(DEFAULT_METRIC_SAMPLE);
  const [candidates, setCandidates] = useState<MetricCandidate[]>([]);
  const [recognizeMessage, setRecognizeMessage] = useState("");
  const measurementValid = /^[a-z][a-z0-9_]{0,63}$/.test(measurement) && measurement !== "node_metrics" && measurement !== "network_probe";

  function toggleEnabled() {
    const next = !enabled;
    onEnabledChange(next);
    if (next && metrics.length === 0) onMetricsChange([emptyMetric()]);
  }

  function updateMetric(index: number, patch: Partial<TaskMetricDefinition>) {
    onMetricsChange(metrics.map((metric, metricIndex) => metricIndex === index ? { ...metric, ...patch } : metric));
  }

  function recognize() {
    const found = recognizeMetricCandidates(sampleOutput);
    const existingExpressions = new Set(metrics.map((metric) => metric.extractor.expression));
    const fresh = found.filter((candidate) => !existingExpressions.has(candidate.expression));
    setCandidates(fresh);
    setRecognizeMessage(found.length === 0 ? "没有识别到数值指标，请检查输出是否为 JSON 或 key=value 格式。" : fresh.length === 0 ? "识别到的指标已经全部在编排列表中。" : `识别到 ${fresh.length} 个可添加指标。`);
  }

  function addCandidate(candidate: MetricCandidate) {
    const used = new Set(metrics.map((metric) => metric.key));
    let key = candidate.key;
    let suffix = 2;
    while (used.has(key)) {
      key = `${candidate.key.slice(0, 60)}_${suffix}`;
      suffix++;
    }
    onMetricsChange([...metrics, {
      key,
      name: candidate.key,
      extractor: { type: candidate.type, expression: candidate.expression },
      unit: suggestedMetricUnit(candidate.key),
    }]);
    setCandidates((current) => current.filter((item) => item.expression !== candidate.expression));
  }

  function addAllCandidates() {
    let next = [...metrics];
    const used = new Set(next.map((metric) => metric.key));
    for (const candidate of candidates) {
      let key = candidate.key;
      let suffix = 2;
      while (used.has(key)) {
        key = `${candidate.key.slice(0, 60)}_${suffix}`;
        suffix++;
      }
      used.add(key);
      next = [...next, { key, name: candidate.key, extractor: { type: candidate.type, expression: candidate.expression }, unit: suggestedMetricUnit(candidate.key) }];
    }
    onMetricsChange(next);
    setCandidates([]);
  }

  return (
    <div className="overflow-hidden rounded-xl border border-primary/15 bg-card shadow-sm">
      <div className="flex flex-wrap items-center gap-3 border-b bg-gradient-to-r from-primary/[0.065] via-card to-card px-4 py-3">
        <div className="grid h-9 w-9 place-items-center rounded-xl bg-primary/10 text-primary"><Database className="h-4 w-4" /></div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2"><p className="text-xs font-semibold">指标编排</p><Badge variant={enabled ? "default" : "outline"}>{enabled ? `${metrics.length} 项指标` : "未开启"}</Badge></div>
          <p className="mt-0.5 text-[9px] leading-4 text-muted-foreground">从 Shell 输出提取数值，写入指定的 MTS 表。</p>
        </div>
        <Switch checked={enabled} onCheckedChange={toggleEnabled} label={`${enabled ? "关闭" : "开启"}指标采集`} />
      </div>

      {!enabled ? (
        <div className="flex items-start gap-2.5 px-4 py-3 text-[10px] leading-5 text-muted-foreground">
          <Wand2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-primary" />
          <span>开启后可以指定 MTS 表名称，并编排 Shell 输出中的指标键、展示名称、提取方式和单位。</span>
        </div>
      ) : (
        <div className="space-y-3 p-3 sm:p-4">
          <div className="grid gap-2 rounded-xl border bg-background/55 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(220px,0.7fr)] sm:items-end">
            <label className="space-y-1.5 text-[10px] font-medium text-muted-foreground">
              MTS 表名称（measurement）
              <div className="relative">
                <Database className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input value={measurement} onChange={(event) => onMeasurementChange(event.target.value.toLowerCase())} maxLength={64} spellCheck={false} className={cn("h-9 pl-8 font-mono text-[11px]", !measurementValid && "border-destructive/50 focus-visible:ring-destructive/20")} placeholder={DEFAULT_METRIC_MEASUREMENT} />
              </div>
            </label>
            <div className={cn("rounded-lg border px-3 py-2 text-[9px] leading-4", measurementValid ? "border-success/20 bg-success/[0.045] text-muted-foreground" : "border-destructive/20 bg-destructive/[0.045] text-destructive")}>
              {measurementValid ? <>新数据将写入 <code className="font-mono text-foreground">{measurement}</code>；修改表名不会迁移历史数据。</> : "仅支持小写字母、数字、下划线；不能使用 node_metrics 或 network_probe。"}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <p className="mr-auto text-[10px] font-semibold text-muted-foreground">字段映射</p>
            <Button type="button" variant="outline" size="sm" onClick={() => setSampleOpen((value) => !value)}><Search className="h-3.5 w-3.5" />{sampleOpen ? "收起识别" : "从输出识别"}</Button>
            <Button type="button" variant="outline" size="sm" onClick={() => onMetricsChange([...metrics, emptyMetric()])}><ListPlus className="h-3.5 w-3.5" />添加指标</Button>
          </div>

          {sampleOpen ? <div className="space-y-2 rounded-xl border border-violet-500/20 bg-violet-500/[0.025] p-3">
            <div className="flex items-center justify-between gap-2">
              <div><p className="text-[10px] font-semibold">示例输出识别</p><p className="mt-0.5 text-[9px] text-muted-foreground">粘贴脚本输出，识别 JSON 或 key=value 中的数值字段。</p></div>
              <Button type="button" size="sm" onClick={recognize}><Wand2 className="h-3.5 w-3.5" />识别指标</Button>
            </div>
            <textarea value={sampleOutput} onChange={(event) => setSampleOutput(event.target.value)} rows={6} spellCheck={false} className="w-full resize-y rounded-lg border border-input bg-slate-950 px-3 py-2 font-mono text-[10px] leading-5 text-slate-100 outline-none focus:border-primary/50" />
            {recognizeMessage ? <p className="text-[9px] text-muted-foreground">{recognizeMessage}</p> : null}
            {candidates.length > 0 ? <div className="space-y-1.5">
              <div className="flex items-center justify-between"><span className="text-[9px] text-muted-foreground">识别结果</span><button type="button" onClick={addAllCandidates} className="text-[9px] font-medium text-primary hover:underline">全部添加</button></div>
              <div className="flex flex-wrap gap-1.5">{candidates.map((candidate) => <button key={candidate.expression} type="button" onClick={() => addCandidate(candidate)} className="inline-flex items-center gap-1.5 rounded-md border border-primary/20 bg-card px-2 py-1 font-mono text-[9px] hover:border-primary/40"><Plus className="h-2.5 w-2.5 text-primary" />{candidate.key}<span className="text-muted-foreground">= {candidate.value}</span></button>)}</div>
            </div> : null}
          </div> : null}

          <datalist id="metric-unit-options"><option value="个" /><option value="%" /><option value="ms" /><option value="s" /><option value="B" /><option value="KB" /><option value="MB" /><option value="次/秒" /></datalist>
          <div className="space-y-2">
            {metrics.length === 0 ? <div className="rounded-xl border border-dashed px-4 py-6 text-center text-[10px] text-muted-foreground">还没有指标。可以手动添加，或从示例输出中识别。</div> : null}
            {metrics.map((metric, index) => <div key={index} className="rounded-xl border bg-background/50 p-3">
              <div className="mb-2.5 flex items-center gap-2">
                <Badge variant="outline" className="font-mono">{String(index + 1).padStart(2, "0")}</Badge>
                <span className="text-[9px] text-muted-foreground">写入字段 <code className="font-mono text-foreground/80">{metric.key || "未命名"}</code></span>
                <button type="button" onClick={() => onMetricsChange(metrics.filter((_, metricIndex) => metricIndex !== index))} className="ml-auto grid h-7 w-7 place-items-center rounded-md text-muted-foreground hover:bg-destructive/10 hover:text-destructive" aria-label="删除指标"><Trash2 className="h-3.5 w-3.5" /></button>
              </div>
              <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_112px_90px]">
                <label className="space-y-1 text-[9px] font-medium text-muted-foreground">指标键<Input value={metric.key} onChange={(event) => updateMetric(index, { key: event.target.value.toLowerCase() })} maxLength={64} spellCheck={false} className="h-8 font-mono text-[10px]" placeholder="db_connections" /></label>
                <label className="space-y-1 text-[9px] font-medium text-muted-foreground">展示名称<Input value={metric.name} onChange={(event) => updateMetric(index, { name: event.target.value })} maxLength={64} className="h-8 text-[10px]" placeholder="数据库连接数" /></label>
                <label className="space-y-1 text-[9px] font-medium text-muted-foreground">提取方式<select value={metric.extractor.type} onChange={(event) => updateMetric(index, { extractor: { ...metric.extractor, type: event.target.value as MetricExtractorType } })} className="h-8 w-full rounded-md border border-input bg-card px-2 text-[10px] shadow-sm outline-none focus:ring-2 focus:ring-ring/30"><option value="json_path">JSON 字段</option><option value="key_value">Key=Value</option><option value="regex">正则捕获</option></select></label>
                <label className="space-y-1 text-[9px] font-medium text-muted-foreground">单位<Input value={metric.unit} onChange={(event) => updateMetric(index, { unit: event.target.value })} maxLength={16} list="metric-unit-options" className="h-8 text-[10px]" placeholder="个" /></label>
              </div>
              <label className="mt-2 block space-y-1 text-[9px] font-medium text-muted-foreground">提取表达式
                <Input value={metric.extractor.expression} onChange={(event) => updateMetric(index, { extractor: { ...metric.extractor, expression: event.target.value } })} maxLength={512} spellCheck={false} className="h-8 font-mono text-[10px]" placeholder={metric.extractor.type === "json_path" ? "$.pool.active" : metric.extractor.type === "key_value" ? "pool.active" : "active:\\s+(\\d+)"} />
              </label>
            </div>)}
          </div>
        </div>
      )}
    </div>
  );
}

function TaskEditorDialog({ open, task, pending, error, onClose, onSubmit }: {
  open: boolean; task: TaskDefinition | null; pending: boolean; error?: string; onClose: () => void; onSubmit: (input: TaskInput) => void;
}) {
  const [kind, setKind] = useState<TaskKind>("three_network_probe");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [script, setScript] = useState(DEFAULT_SCRIPT);
  const [targets, setTargets] = useState<ThreeNetworkTarget[]>(defaultNetworkConfig().targets);
  const [probeCount, setProbeCount] = useState(10);
  const [probeIntervalMs, setProbeIntervalMs] = useState(200);
  const [probeTimeoutMs, setProbeTimeoutMs] = useState(1000);
  const [metricEnabled, setMetricEnabled] = useState(false);
  const [metricMeasurement, setMetricMeasurement] = useState(DEFAULT_METRIC_MEASUREMENT);
  const [metrics, setMetrics] = useState<TaskMetricDefinition[]>([]);
  const [intervalSeconds, setIntervalSeconds] = useState(300);
  const [timeoutSeconds, setTimeoutSeconds] = useState(60);
  const [enabled, setEnabled] = useState(false);
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (!open) return;
    const defaults = emptyTask();
    const config = task && isThreeNetworkConfig(task.config) ? task.config : defaultNetworkConfig();
    const metricConfig = task && isShellMetricConfig(task.config) ? task.config.metricSchema : null;
    const byCarrier = new Map(config.targets.map((target) => [target.carrier, target]));
    setName(task?.name ?? defaults.name);
    setDescription(task?.description ?? defaults.description);
    setKind(task?.kind ?? defaults.kind);
    setScript(task?.script || DEFAULT_SCRIPT);
    setTargets(defaultNetworkConfig().targets.map((target) => ({ ...(byCarrier.get(target.carrier) ?? target) })));
    setProbeCount(config.probe.count || 10);
    setProbeIntervalMs(config.probe.intervalMs || 200);
    setProbeTimeoutMs(config.probe.timeoutMs || 1000);
    setMetricEnabled(metricConfig?.enabled ?? false);
    setMetricMeasurement(metricConfig?.measurement || DEFAULT_METRIC_MEASUREMENT);
    setMetrics(metricConfig?.metrics?.map((metric) => ({ ...metric, extractor: { ...metric.extractor } })) ?? []);
    setIntervalSeconds(task?.intervalSeconds ?? defaults.intervalSeconds);
    setTimeoutSeconds(task?.timeoutSeconds ?? defaults.timeoutSeconds);
    setEnabled(task?.enabled ?? defaults.enabled);
    setFormError("");
  }, [open, task]);

  function updateTarget(carrier: Carrier, patch: Partial<ThreeNetworkTarget>) {
    setTargets((current) => current.map((target) => target.carrier === carrier ? { ...target, ...patch } : target));
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setFormError("任务名称不能为空");
      return;
    }
    if (intervalSeconds < 30 || intervalSeconds > 604800 || timeoutSeconds < 1 || timeoutSeconds > 300) {
      setFormError("执行周期需在 30 秒到 7 天之间，超时需在 1 到 300 秒之间");
      return;
    }
    if (kind === "three_network_probe") {
      for (const target of targets) {
        if (!target.host.trim()) {
          setFormError(`${carrierMeta(target.carrier).short}目标地址不能为空`);
          return;
        }
        if (target.port < 1 || target.port > 65535) {
          setFormError(`${carrierMeta(target.carrier).short} TCP 端口必须在 1 到 65535 之间`);
          return;
        }
      }
      if (probeCount < 1 || probeCount > 30 || probeIntervalMs < 50 || probeIntervalMs > 5000 || probeTimeoutMs < 100 || probeTimeoutMs > 10000) {
        setFormError("探测次数需为 1–30，间隔需为 50–5000ms，单次超时需为 100–10000ms");
        return;
      }
      const minimumTaskTimeout = Math.ceil(probeCount * (probeIntervalMs + probeTimeoutMs) / 1000);
      if (timeoutSeconds < minimumTaskTimeout) {
        setFormError(`任务超时至少需要 ${minimumTaskTimeout} 秒，才能覆盖当前 TCP 探测配置`);
        return;
      }
      setFormError("");
      onSubmit({
        name: name.trim(), description: description.trim(), kind, script: "", intervalSeconds, timeoutSeconds, enabled,
        config: {
          targets: targets.map((target) => ({ ...target, host: target.host.trim(), label: target.label.trim() || carrierMeta(target.carrier).short, protocol: "tcp" })),
          probe: { count: probeCount, intervalMs: probeIntervalMs, timeoutMs: probeTimeoutMs },
        },
      });
      return;
    }
    if (!script.trim()) {
      setFormError("Shell 脚本不能为空");
      return;
    }
    const normalizedMetrics = metrics.map((metric) => ({
      ...metric,
      key: metric.key.trim(),
      name: metric.name.trim() || metric.key.trim(),
      unit: metric.unit.trim(),
      extractor: { ...metric.extractor, expression: metric.extractor.expression.trim() },
    }));
    if (metricEnabled) {
      if (!/^[a-z][a-z0-9_]{0,63}$/.test(metricMeasurement) || metricMeasurement === "node_metrics" || metricMeasurement === "network_probe") {
        setFormError("MTS 表名称只能使用小写字母、数字和下划线，且不能占用系统内置表");
        return;
      }
      if (normalizedMetrics.length === 0) {
        setFormError("开启指标采集后至少需要配置一个指标");
        return;
      }
      const keys = new Set<string>();
      for (const metric of normalizedMetrics) {
        if (!/^[a-z][a-z0-9_]{0,63}$/.test(metric.key)) {
          setFormError("指标键只能使用小写字母、数字和下划线，且必须以字母开头");
          return;
        }
        if (keys.has(metric.key)) {
          setFormError(`指标键「${metric.key}」重复`);
          return;
        }
        keys.add(metric.key);
        if (!metric.extractor.expression) {
          setFormError(`指标「${metric.key}」的提取表达式不能为空`);
          return;
        }
        if (metric.extractor.type === "json_path" && !metric.extractor.expression.startsWith("$")) {
          setFormError(`指标「${metric.key}」的 JSON 路径必须以 $ 开头`);
          return;
        }
        if (metric.extractor.type === "regex") {
          try {
            new RegExp(metric.extractor.expression);
          } catch {
            setFormError(`指标「${metric.key}」的正则表达式无效`);
            return;
          }
        }
      }
    }
    setFormError("");
    onSubmit({
      name: name.trim(),
      description: description.trim(),
      kind: "shell_script",
      script,
      config: { metricSchema: { enabled: metricEnabled, measurement: metricMeasurement.trim() || DEFAULT_METRIC_MEASUREMENT, metrics: normalizedMetrics } },
      intervalSeconds,
      timeoutSeconds,
      enabled,
    });
  }

  return (
    <Modal open={open} onClose={onClose} title={task ? "编辑任务" : "新建任务"} description="选择任务模板并配置执行参数。Server 通过 gRPC 双向流将任务下发给 Agent。" className="max-w-6xl" footer={<>
      <button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button>
      <button type="submit" form="task-editor-form" disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{task ? "保存配置" : "创建任务"}</button>
    </>}>
      <form id="task-editor-form" onSubmit={submit} className="space-y-5">
        <div>
          <p className="mb-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">任务类型</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <button type="button" disabled={Boolean(task)} onClick={() => setKind("three_network_probe")} className={cn("group flex items-start gap-3 rounded-xl border p-3.5 text-left transition-all", kind === "three_network_probe" ? "border-primary/35 bg-primary/[0.055] shadow-sm" : "bg-background/40 hover:border-primary/20", task && task.kind !== "three_network_probe" && "opacity-45")}>
              <span className={cn("grid h-10 w-10 shrink-0 place-items-center rounded-xl", kind === "three_network_probe" ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground")}><Network className="h-4 w-4" /></span>
              <span className="min-w-0 flex-1"><span className="flex items-center gap-2 text-xs font-semibold">三网时延与丢包检测<Badge variant="outline" className="px-1.5 py-0 text-[8px]">TCP</Badge></span><span className="mt-1 block text-[10px] leading-5 text-muted-foreground">配置电信、联通、移动三个目标，采集 TCP 建连时延、失败率、P95 与抖动。</span></span>
            </button>
            <button type="button" disabled={Boolean(task)} onClick={() => setKind("shell_script")} className={cn("group flex items-start gap-3 rounded-xl border p-3.5 text-left transition-all", kind === "shell_script" ? "border-primary/35 bg-primary/[0.055] shadow-sm" : "bg-background/40 hover:border-primary/20", task && task.kind !== "shell_script" && "opacity-45")}>
              <span className={cn("grid h-10 w-10 shrink-0 place-items-center rounded-xl", kind === "shell_script" ? "bg-violet-500 text-white" : "bg-muted text-muted-foreground")}><Terminal className="h-4 w-4" /></span>
              <span className="min-w-0 flex-1"><span className="flex items-center gap-2 text-xs font-semibold">自定义 Shell 脚本</span><span className="mt-1 block text-[10px] leading-5 text-muted-foreground">编写脚本，由 Agent 使用 /bin/sh 执行并回传输出、退出码与耗时。</span></span>
            </button>
          </div>
          {task ? <p className="mt-2 text-[9px] text-muted-foreground">任务类型创建后不可修改；如需更换类型，请新建任务。</p> : null}
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <label className="space-y-1.5 text-xs font-medium text-muted-foreground">任务名称<Input autoFocus value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="例如：三网质量巡检" /></label>
          <label className="space-y-1.5 text-xs font-medium text-muted-foreground">任务说明<Input value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} placeholder="说明任务用途和结果含义" /></label>
        </div>

        {kind === "three_network_probe" ? <div className="space-y-3 rounded-2xl border bg-muted/[0.16] p-3.5 sm:p-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div><p className="flex items-center gap-2 text-xs font-semibold"><Globe2 className="h-3.5 w-3.5 text-primary" />三网探测目标</p><p className="mt-1 text-[10px] text-muted-foreground">每个运营商配置一个目标地址；默认通过 TCP Connect 完成三次握手并记录 RTT。</p></div>
            <span className="inline-flex w-fit items-center gap-1.5 rounded-full border border-primary/20 bg-primary/[0.06] px-2.5 py-1 text-[9px] font-semibold text-primary"><Activity className="h-3 w-3" />TCP Connect</span>
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            {CARRIERS.map((carrier) => {
              const target = targets.find((item) => item.carrier === carrier.id) ?? defaultNetworkConfig().targets.find((item) => item.carrier === carrier.id)!;
              return <div key={carrier.id} className={cn("rounded-xl border p-3", carrier.accent)}>
                <div className="flex items-center gap-2"><span className={cn("h-2 w-2 rounded-full", carrier.dot)} /><span className="text-[11px] font-semibold">{carrier.name}</span><span className="ml-auto rounded border bg-card/80 px-1.5 py-0.5 text-[8px] font-medium text-muted-foreground">TCP</span></div>
                <label className="mt-3 block space-y-1 text-[9px] font-medium text-muted-foreground">目标主机 / IP<Input value={target.host} onChange={(event) => updateTarget(carrier.id, { host: event.target.value })} className="h-8 font-mono text-[11px]" placeholder="IP 或域名" /></label>
                <div className="mt-2 grid grid-cols-[90px_minmax(0,1fr)] gap-2">
                  <label className="block space-y-1 text-[9px] font-medium text-muted-foreground">TCP 端口<Input type="number" min={1} max={65535} value={target.port} onChange={(event) => updateTarget(carrier.id, { port: Number(event.target.value) })} className="h-8 font-mono text-[11px]" /></label>
                  <label className="block space-y-1 text-[9px] font-medium text-muted-foreground">目标名称<Input value={target.label} onChange={(event) => updateTarget(carrier.id, { label: event.target.value })} maxLength={64} className="h-8 text-[10px]" /></label>
                </div>
                <label className="mt-2 block space-y-1 text-[9px] font-medium text-muted-foreground">地址族<select value={target.addressFamily} onChange={(event) => updateTarget(carrier.id, { addressFamily: event.target.value as AddressFamily })} className="h-8 w-full rounded-md border border-input bg-card px-2 text-[10px] shadow-sm outline-none focus:ring-2 focus:ring-ring/30"><option value="auto">自动选择（优先 IPv4）</option><option value="ipv4">仅 IPv4</option><option value="ipv6">仅 IPv6</option></select></label>
              </div>;
            })}
          </div>
          <div className="grid gap-3 rounded-xl border bg-card/65 p-3 sm:grid-cols-3">
            <label className="space-y-1 text-[9px] font-medium text-muted-foreground">探测次数<Input type="number" min={1} max={30} value={probeCount} onChange={(event) => setProbeCount(Number(event.target.value))} className="h-8 text-[11px]" /></label>
            <label className="space-y-1 text-[9px] font-medium text-muted-foreground">探测间隔（ms）<Input type="number" min={50} max={5000} value={probeIntervalMs} onChange={(event) => setProbeIntervalMs(Number(event.target.value))} className="h-8 text-[11px]" /></label>
            <label className="space-y-1 text-[9px] font-medium text-muted-foreground">单次超时（ms）<Input type="number" min={100} max={10000} value={probeTimeoutMs} onChange={(event) => setProbeTimeoutMs(Number(event.target.value))} className="h-8 text-[11px]" /></label>
          </div>
          <div className="flex items-start gap-2 rounded-lg border border-primary/15 bg-primary/[0.035] px-3 py-2 text-[9px] leading-5 text-muted-foreground"><ArrowDownUp className="mt-0.5 h-3 w-3 shrink-0 text-primary" /><span>TCP 模式以三次握手完成时间作为 RTT；连接失败计入丢包/失败率。若目标端口关闭，即使网络可达也会被计为失败，请使用实际开放的服务端口。</span></div>
        </div> : <div className="space-y-3">
          <label className="block space-y-1.5 text-xs font-medium text-muted-foreground">Shell 脚本
            <textarea value={script} onChange={(event) => setScript(event.target.value)} rows={9} spellCheck={false} className="mt-1.5 flex w-full resize-y rounded-md border border-input bg-slate-950 px-3 py-2.5 font-mono text-[11px] leading-5 text-slate-100 shadow-sm outline-none transition-colors placeholder:text-slate-500 focus:border-primary/50 focus:ring-2 focus:ring-primary/10" placeholder="#!/bin/sh" />
          </label>
          <div className="flex items-start gap-2 rounded-lg border border-warning/25 bg-warning/[0.055] p-3 text-[10px] leading-5 text-warning"><ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" /><span>脚本会以 Agent 进程用户权限执行；若 Agent 以 root 运行，则脚本也拥有 root 权限。仅允许可信管理员创建任务。</span></div>
          <MetricOrchestration
            enabled={metricEnabled}
            measurement={metricMeasurement}
            metrics={metrics}
            onEnabledChange={setMetricEnabled}
            onMeasurementChange={setMetricMeasurement}
            onMetricsChange={setMetrics}
          />
        </div>}

        <div className="grid items-end gap-4 rounded-xl border bg-background/40 p-3 sm:grid-cols-[1fr_1fr_auto]">
          <label className="space-y-1.5 text-[10px] font-medium text-muted-foreground">执行周期（秒）<Input type="number" min={30} max={604800} value={intervalSeconds} onChange={(event) => setIntervalSeconds(Number(event.target.value))} /></label>
          <label className="space-y-1.5 text-[10px] font-medium text-muted-foreground">任务超时（秒）<Input type="number" min={1} max={300} value={timeoutSeconds} onChange={(event) => setTimeoutSeconds(Number(event.target.value))} /></label>
          <label className="flex h-9 items-center gap-2 whitespace-nowrap text-[11px] font-medium"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} className="h-3.5 w-3.5 accent-primary" />{task ? "保存后启用" : "创建后立即启用"}</label>
        </div>
        {formError || error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{formError || error}</div> : null}
      </form>
    </Modal>
  );
}

function ScriptDialog({ task, onClose }: { task: TaskDefinition | null; onClose: () => void }) {
  return (
    <Modal open={Boolean(task)} onClose={onClose} title={`${task?.name ?? ""} · 脚本`} description="任务脚本会在所有已纳管 Agent 上周期执行。" className="max-w-3xl" footer={<button type="button" onClick={onClose} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70">关闭</button>}>
      <pre className="max-h-[520px] overflow-auto rounded-xl bg-slate-950 p-4 font-mono text-[11px] leading-5 text-slate-100 scrollbar-thin">{task?.script}</pre>
    </Modal>
  );
}

function parseNetworkProbeResult(output?: string): NetworkProbeResult | null {
  if (!output) return null;
  try {
    const parsed = JSON.parse(output) as LegacyProbeResult;
    if (parsed.kind !== "three_network_probe" || !Array.isArray(parsed.targets)) return null;
    return {
      kind: "three_network_probe",
      startedAt: parsed.startedAt ?? "",
      durationMs: parsed.durationMs ?? 0,
      targets: parsed.targets.map(normalizeProbeTarget),
    };
  } catch {
    return null;
  }
}

type LegacyProbeTarget = Partial<NetworkProbeTargetResult> & {
  sent?: number;
  received?: number;
  lossPercent?: number;
  rtt?: { minMs?: number; avgMs?: number; maxMs?: number; p50Ms?: number; p95Ms?: number; jitterMs?: number };
};

type LegacyProbeResult = Partial<Omit<NetworkProbeResult, "targets">> & { targets?: LegacyProbeTarget[] };

function normalizeProbeTarget(target: LegacyProbeTarget): NetworkProbeTargetResult {
  const sent = target.loss?.sent ?? target.sent ?? 0;
  const received = target.loss?.received ?? target.received ?? 0;
  const legacyAverage = target.rtt?.avgMs ?? 0;
  const loss = target.loss ?? {
    available: sent > 0,
    sent,
    received,
    lost: Math.max(0, sent - received),
    failed: 0,
    percent: sent > 0 ? Math.max(0, (sent - received) * 100 / sent) : null,
  };
  const legacyLatencyAvailable = !target.latency && legacyAverage > 0;
  const latency = target.latency ?? {
    available: legacyLatencyAvailable,
    sampleCount: legacyLatencyAvailable ? received : 0,
    minMs: legacyLatencyAvailable ? target.rtt?.minMs ?? null : null,
    avgMs: legacyLatencyAvailable ? target.rtt?.avgMs ?? null : null,
    maxMs: legacyLatencyAvailable ? target.rtt?.maxMs ?? null : null,
    p50Ms: legacyLatencyAvailable ? target.rtt?.p50Ms ?? null : null,
    p95Ms: legacyLatencyAvailable ? target.rtt?.p95Ms ?? null : null,
    jitterMs: legacyLatencyAvailable ? target.rtt?.jitterMs ?? null : null,
  };
  return {
    carrier: target.carrier ?? "telecom",
    label: target.label ?? "",
    host: target.host ?? "",
    port: target.port ?? 0,
    protocol: target.protocol ?? "tcp",
    family: target.family ?? "auto",
    resolvedIp: target.resolvedIp ?? "",
    resolveMs: target.resolveMs ?? 0,
    loss,
    latency,
    samples: Array.isArray(target.samples) ? target.samples : [],
    status: target.status ?? "failed",
    errors: target.errors,
  };
}

function runOutputPreview(run: TaskRun): string {
  if (run.status === "delivered") return "任务已下发，等待 Agent 回传结果。";
  const result = parseNetworkProbeResult(run.output);
  if (result) {
    return result.targets.map((target) => `${carrierMeta(target.carrier).short} ${probeTargetStatusLabel(target.status)} · 丢包 ${formatLossPercent(target)} · 时延 ${formatProbeLatency(target)}`).join("  ·  ");
  }
  if (run.output.includes('"kind":"three_network_probe"')) return "三网探测已返回结构化结果，点击查看丢包与时延明细。";
  return run.output || "无输出";
}

function ProbeResultView({ result, rawOutput }: { result: NetworkProbeResult; rawOutput: string }) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/[0.16] px-3 py-2.5">
        <span className="grid h-7 w-7 place-items-center rounded-lg bg-primary/10 text-primary"><Network className="h-3.5 w-3.5" /></span>
        <div><p className="text-[11px] font-semibold">三网 TCP 探测结果</p><p className="text-[9px] text-muted-foreground">总耗时 {formatDuration(result.durationMs)} · {result.targets.length} 个运营商目标</p></div>
        <Badge variant="default" className="ml-auto">TCP Connect</Badge>
      </div>
      <div className="grid gap-3 lg:grid-cols-3">
        {result.targets.map((target) => {
          const meta = carrierMeta(target.carrier);
          const variant = target.status === "ok" ? "success" : target.status === "degraded" ? "warning" : "destructive";
          return <div key={target.carrier} className="rounded-xl border bg-background/45 p-3">
            <div className="flex items-center gap-2"><span className={cn("h-2 w-2 rounded-full", meta.dot)} /><span className="text-[11px] font-semibold">{meta.name}</span><Badge variant={variant} className="ml-auto px-1.5 py-0 text-[8px]">{probeTargetStatusLabel(target.status)}</Badge></div>
            <p className="mt-2 truncate font-mono text-[10px] text-foreground/85">{target.host}:{target.port}</p>
            <p className="mt-0.5 truncate font-mono text-[9px] text-muted-foreground">{target.resolvedIp || "解析失败"} · {target.family.toUpperCase()} · DNS {formatLatency(target.resolveMs)}</p>
            <div className="mt-3 grid grid-cols-2 gap-2">
              <ProbeDimension label="丢包" value={formatLossPercent(target)} note={`${target.loss.sent} 发送 · ${target.loss.received} 接收 · ${target.loss.lost} 丢失 · ${target.loss.failed} 失败`} tone={!target.loss.available ? "muted" : target.loss.lost > 0 || target.loss.failed > 0 ? "warning" : "success"} />
              <ProbeDimension label="时延" value={formatProbeLatency(target)} note={probeLatencyNote(target)} tone={!target.latency.available ? "muted" : target.loss.lost > 0 || target.loss.failed > 0 ? "warning" : "success"} />
            </div>
            <div className="mt-1.5 grid grid-cols-3 gap-1.5">
              <ProbeMetric label="P95" value={formatLatency(target.latency.p95Ms)} />
              <ProbeMetric label="抖动" value={formatLatency(target.latency.jitterMs)} />
              <ProbeMetric label="最小 / 最大" value={target.latency.available ? `${formatLatency(target.latency.minMs)} / ${formatLatency(target.latency.maxMs)}` : "不可测"} />
            </div>
            {target.samples.length ? <div className="mt-2.5"><p className="text-[7px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">逐次探测</p><div className="mt-1 flex flex-wrap gap-1">{target.samples.map((sample) => <span key={sample.index} title={sample.error} className={cn("rounded border px-1.5 py-0.5 font-mono text-[8px] tabular-nums", sample.outcome === "success" ? "border-success/15 bg-success/[0.045] text-success" : sample.outcome === "lost" ? "border-warning/15 bg-warning/[0.05] text-warning" : "border-destructive/15 bg-destructive/[0.045] text-destructive")}>#{sample.index} {sample.outcome === "success" ? formatLatency(sample.rttMs) : sample.outcome === "lost" ? "丢失" : "失败"}</span>)}</div></div> : null}
            {target.errors?.length ? <p className="mt-2 line-clamp-2 break-words rounded-md bg-destructive/[0.04] px-2 py-1.5 font-mono text-[8px] leading-4 text-destructive">{target.errors[0]}</p> : null}
          </div>;
        })}
      </div>
      <details className="rounded-xl border bg-slate-950 text-slate-100">
        <summary className="cursor-pointer px-3 py-2 text-[9px] text-slate-400">查看原始 JSON</summary>
        <pre className="max-h-72 overflow-auto border-t border-slate-800 p-3 font-mono text-[9px] leading-5 scrollbar-thin">{rawOutput}</pre>
      </details>
    </div>
  );
}

function ProbeMetric({ label, value, tone = "default" }: { label: string; value: string; tone?: "default" | "success" | "warning" }) {
  return <div className="rounded-lg bg-muted/45 px-2 py-1.5"><p className="text-[7px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">{label}</p><p className={cn("mt-0.5 truncate text-[10px] font-semibold tabular-nums", tone === "success" && "text-success", tone === "warning" && "text-warning")}>{value}</p></div>;
}

function ProbeDimension({ label, value, note, tone }: { label: string; value: string; note: string; tone: "success" | "warning" | "muted" }) {
  return <div className={cn("rounded-lg border px-2 py-1.5", tone === "success" ? "border-success/15 bg-success/[0.035]" : tone === "warning" ? "border-warning/20 bg-warning/[0.04]" : "border-border bg-muted/35")}><p className="text-[7px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">{label}</p><p className={cn("mt-0.5 truncate text-[15px] font-semibold tabular-nums", tone === "success" && "text-success", tone === "warning" && "text-warning", tone === "muted" && "text-muted-foreground")}>{value}</p><p className="mt-0.5 line-clamp-2 text-[8px] leading-4 text-muted-foreground">{note}</p></div>;
}

function RunDetailDialog({ run, detail, loading, error, onClose }: { run: TaskRun | null; detail?: TaskRun; loading: boolean; error?: string; onClose: () => void }) {
  const data = detail ?? run;
  const probeResult = parseNetworkProbeResult(data?.output);
  return (
    <Modal open={Boolean(run)} onClose={onClose} title={`${run?.taskName ?? ""} · 执行结果`} description={run ? `${run.nodeName || run.nodeId} · ${formatTime(run.completedAt || run.scheduledAt)}` : undefined} className={probeResult ? "max-w-5xl" : "max-w-3xl"} footer={<button type="button" onClick={onClose} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70">关闭</button>}>
      {loading ? <LoadingState label="正在读取完整输出…" /> : error ? <ErrorState message={error} /> : (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <TaskMeta label="状态" value={runStatusLabel(data?.status ?? "delivered")} />
            <TaskMeta label="退出码" value={String(data?.exitCode ?? 0)} />
            <TaskMeta label="耗时" value={formatDuration(data?.durationMs ?? 0)} />
            <TaskMeta label="执行超时" value={`${data?.timeoutSeconds ?? 0}s`} />
          </div>
          {data?.error ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[10px] leading-5 text-destructive">{data.error}</div> : null}
          {probeResult ? <ProbeResultView result={probeResult} rawOutput={data?.output ?? ""} /> : <pre className="max-h-[460px] min-h-32 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-slate-950 p-4 font-mono text-[11px] leading-5 text-slate-100 scrollbar-thin">{data?.output || "无输出"}</pre>}
        </div>
      )}
    </Modal>
  );
}

function DeleteTaskDialog({ task, pending, error, onClose, onConfirm }: { task: TaskDefinition | null; pending: boolean; error?: string; onClose: () => void; onConfirm: () => void }) {
  return (
    <Modal open={Boolean(task)} onClose={onClose} title="删除任务" description="删除后不会再向 Agent 下发；已产生的执行记录会继续保留。" footer={<>
      <button type="button" onClick={onClose} disabled={pending} className="inline-flex h-9 items-center rounded-md border bg-card px-4 text-sm font-medium hover:bg-muted/70 disabled:opacity-50">取消</button>
      <button type="button" onClick={onConfirm} disabled={pending} className="inline-flex h-9 items-center gap-2 rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50">{pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}确认删除</button>
    </>}>
      <div className="rounded-lg border border-destructive/20 bg-destructive/[0.045] p-3 text-xs leading-6 text-muted-foreground">确定删除任务 <strong className="text-foreground">「{task?.name}」</strong>？该操作不会删除历史执行结果。</div>
      {error ? <div className="mt-3 rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{error}</div> : null}
    </Modal>
  );
}

function runStatusLabel(status: TaskRunStatus): string {
  if (status === "success") return "成功";
  if (status === "failed") return "失败";
  if (status === "timeout") return "超时";
  return "已下发";
}

function probeTargetStatusLabel(status: "ok" | "degraded" | "failed"): string {
  if (status === "ok") return "正常";
  if (status === "degraded") return "有丢包";
  return "失败";
}

function formatLossPercent(target: NetworkProbeTargetResult): string {
  if (!target.loss.available || target.loss.percent == null) return "不可用";
  const percent = target.loss.percent;
  return `${percent.toFixed(percent > 0 && percent < 10 ? 1 : 0)}%`;
}

function formatProbeLatency(target: NetworkProbeTargetResult): string {
  if (!target.latency.available) {
    if (target.loss.lost > 0 && target.loss.received === 0) return "不可测（全部丢包）";
    if (target.loss.failed > 0 && target.loss.received === 0) return "不可测（连接失败）";
    return "不可用";
  }
  return formatLatency(target.latency.avgMs);
}

function probeLatencyNote(target: NetworkProbeTargetResult): string {
  if (!target.latency.available) {
    if (target.loss.lost > 0 && target.loss.received === 0) return "没有收到握手响应";
    if (target.loss.failed > 0 && target.loss.received === 0) return "TCP 连接未建立";
    return "缺少有效探测请求";
  }
  if (target.loss.lost > 0 || target.loss.failed > 0) return `基于 ${target.latency.sampleCount} 个成功请求`;
  return `${target.latency.sampleCount} 个成功样本`;
}

function formatLatency(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return "—";
  return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
}

function formatInterval(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400} 天`;
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}

function formatDuration(value: number): string {
  if (!value) return "—";
  if (value < 1000) return `${value} ms`;
  return `${(value / 1000).toFixed(1)} s`;
}

function invalidateTasks(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: ["tasks"] });
  void queryClient.invalidateQueries({ queryKey: ["task-runs"] });
}
