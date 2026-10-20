import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
import { formatTime } from "@/lib/intl-format"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { getRuntimeStatus } from "@/lib/runtime-api"
import type { RuntimeStatus, RuntimeStream, RuntimeWorker } from "@/types/runtime"

const refreshMilliseconds = 10000
// internal/metricsfanout.StaleWindow；仅用于把有界的快照年龄显示为比例。
const staleWindowSeconds = 30
const workerLabelKeys: Record<string, string> = {
  alerts: "operations.worker.alerts",
  traffic_reports: "operations.worker.traffic_reports",
  maintenance: "operations.worker.maintenance",
  metrics_fanout: "operations.worker.metrics_fanout",
  notifications: "operations.worker.notifications",
  metric_retention: "operations.worker.metric_retention",
  node_contacts: "operations.worker.node_contacts",
  remote_operations: "operations.worker.remote_operations",
  geoip: "operations.worker.geoip",
  node_lifecycle: "operations.worker.node_lifecycle",
  accelerators: "operations.worker.accelerators",
  agent_rollouts: "operations.worker.agent_rollouts",
}
const workerStateLabelKeys: Record<string, string> = {
  healthy: "operations.worker_state.healthy",
  degraded: "operations.worker_state.degraded",
  starting: "operations.worker_state.starting",
  stopped: "operations.worker_state.stopped",
  failed: "operations.worker_state.failed",
}

type Translate = (key: string, params?: Record<string, string | number>) => string

// 后端只返回稳定标识；目录缺项时原样显示标识，便于发现新增枚举。
function label(t: Translate, keys: Record<string, string>, value: string) {
  return t(keys[value] ?? value)
}
function percent(value: number, bound: number) {
  if (!Number.isFinite(value) || !Number.isFinite(bound) || bound <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((value / bound) * 100)))
}
function relativeAge(stamp: string, now: number, t: Translate) {
  const at = Date.parse(stamp)
  if (!Number.isFinite(at) || at <= 0) return "—"
  const seconds = Math.max(0, Math.round((now - at) / 1000))
  if (seconds < 60) return t("operations.relative_seconds", { count: seconds })
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return t("operations.relative_minutes", { count: minutes })
  return t("operations.relative_hours", { count: Math.round(minutes / 60) })
}
function workerVariant(worker: RuntimeWorker) {
  if (worker.state === "healthy") return "default" as const
  return worker.state === "starting" ? "secondary" as const : "destructive" as const
}
export function isRuntimeStatus(value: unknown): value is RuntimeStatus {
  if (!value || typeof value !== "object") return false
  const candidate = value as RuntimeStatus
  return Array.isArray(candidate.workers) && !!candidate.metrics && typeof candidate.metrics.age_seconds === "number"
}
function degradation(status: RuntimeStatus, t: Translate) {
  const reasons: string[] = []
  if (status.stopping) reasons.push(t("operations.the_server_is_shutting_down"))
  const inactive = status.workers.filter((worker) => worker.configured && !worker.running).length
  if (inactive > 0) reasons.push(t("operations.workers_not_running", { count: inactive }))
  const failing = status.workers.filter((worker) => worker.running && worker.consecutive_failures > 0).length
  if (failing > 0) reasons.push(t("operations.workers_keep_failing", { count: failing }))
  const stream = status.metrics
  if (stream.closed) reasons.push(t("operations.the_metrics_stream_is_closed"))
  else if (!stream.available) reasons.push(t("operations.the_metrics_snapshot_is_unavailable"))
  else if (stream.stale) reasons.push(t("operations.the_metrics_snapshot_is_stale"))
  return reasons
}

export function OperationRuntime() {
  const { t } = useLocale()
  const resource = useOperationResource(getRuntimeStatus, refreshMilliseconds)
  const status = isRuntimeStatus(resource.data) ? resource.data : undefined
  const reasons = status ? degradation(status, t) : []
  return <section className="space-y-4">
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 className="text-lg font-semibold">{t("operations.runtime")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("operations.read_only_server_scheduler_and_metrics_stream_st")}</p>
      </div>
      <Button variant="outline" size="sm" onClick={resource.reload}>{t("operations.refresh")}</Button>
    </header>
    {resource.error && <Alert variant="destructive"><AlertDescription>{t("operations.runtime_status_is_unavailable_try_again_later")}</AlertDescription></Alert>}
    {!status && resource.loading && <RuntimeSkeleton />}
    {status && <>
      {reasons.length > 0 && <Alert variant="destructive"><AlertTitle>{t("operations.runtime_degraded")}</AlertTitle><AlertDescription>{reasons.join(t("operations.reason_separator"))}</AlertDescription></Alert>}
      <Tabs defaultValue="workers"><TabsList><TabsTrigger value="workers">{t("operations.workers")}</TabsTrigger><TabsTrigger value="stream">{t("operations.metrics_stream")}</TabsTrigger></TabsList>
        <TabsContent value="workers" className="pt-3"><WorkerTable workers={[...status.workers].sort((left, right) => left.name.localeCompare(right.name))} /></TabsContent>
        <TabsContent value="stream" className="pt-3"><StreamTable stream={status.metrics} /></TabsContent>
      </Tabs>
    </>}
  </section>
}

function RuntimeSkeleton() {
  const { t } = useLocale()
  return <div role="status" aria-label={t("operations.loading_runtime_status")} className="space-y-2"><Skeleton className="h-6 w-40" /><Skeleton className="h-32 w-full" /></div>
}

function WorkerTable({ workers }: { workers: RuntimeWorker[] }) {
  const { t } = useLocale()
  const now = Date.now()
  return <Table><TableHeader><TableRow>
    <TableHead>{t("operations.worker")}</TableHead><TableHead>{t("operations.state")}</TableHead><TableHead>{t("operations.last_success")}</TableHead>
    <TableHead>{t("operations.consecutive_failures")}</TableHead><TableHead>{t("operations.restarts")}</TableHead><TableHead>{t("operations.scheduler_lag")}</TableHead>
  </TableRow></TableHeader><TableBody>{workers.map((worker) => <TableRow key={worker.name}>
    <TableCell className="font-medium">{label(t, workerLabelKeys, worker.name)}</TableCell>
    <TableCell><Badge variant={workerVariant(worker)}>{label(t, workerStateLabelKeys, worker.state)}</Badge></TableCell>
    <TableCell>{relativeAge(worker.last_success, now, t)}</TableCell>
    <TableCell className="tabular-nums">{worker.consecutive_failures}</TableCell>
    <TableCell className="tabular-nums">{worker.restarts}</TableCell>
    <TableCell className="min-w-40">{worker.interval_seconds > 0 ? <div className="space-y-1"><span className="font-mono text-xs tabular-nums">{worker.scheduler_lag_seconds.toFixed(1)}s / {worker.interval_seconds}s</span><Progress value={percent(worker.scheduler_lag_seconds, worker.interval_seconds)} aria-label={t("operations.scheduler_lag")} /></div> : "—"}</TableCell>
  </TableRow>)}</TableBody></Table>
}

function StreamTable({ stream }: { stream: RuntimeStream }) {
  const { locale, t } = useLocale()
  const healthy = stream.available && !stream.stale && !stream.closed
  const state = stream.closed ? (t("operations.closed")) : !stream.available ? (t("operations.unavailable")) : stream.stale ? (t("operations.stale")) : (t("operations.available"))
  const generated = Number.isFinite(Date.parse(stream.timestamp)) ? formatTime(locale, stream.timestamp) : "—"
  return <Table><TableHeader><TableRow><TableHead>{t("operations.metric")}</TableHead><TableHead>{t("operations.value")}</TableHead></TableRow></TableHeader><TableBody>
    <TableRow><TableCell>{t("operations.snapshot_state")}</TableCell><TableCell><Badge variant={healthy ? "default" : "destructive"}>{state}</Badge></TableCell></TableRow>
    <TableRow><TableCell>{t("operations.generation")}</TableCell><TableCell className="font-mono tabular-nums">{stream.generation}</TableCell></TableRow>
    <TableRow><TableCell>{t("operations.snapshot_age")}</TableCell><TableCell className="min-w-40"><div className="space-y-1"><span className="font-mono text-xs tabular-nums">{stream.age_seconds.toFixed(1)}s / {staleWindowSeconds}s</span><Progress value={percent(stream.age_seconds, staleWindowSeconds)} aria-label={t("operations.snapshot_age")} /></div></TableCell></TableRow>
    <TableRow><TableCell>{t("operations.connections_reserved")}</TableCell><TableCell className="font-mono tabular-nums">{stream.connections} / {stream.reserved}</TableCell></TableRow>
    <TableRow><TableCell>{t("operations.builds_failures")}</TableCell><TableCell className="font-mono tabular-nums">{stream.builds} / {stream.failures}</TableCell></TableRow>
    <TableRow><TableCell>{t("operations.dropped_skipped")}</TableCell><TableCell className="font-mono tabular-nums">{stream.dropped} / {stream.skipped_cadences}</TableCell></TableRow>
    <TableRow><TableCell>{t("operations.build_duration")}</TableCell><TableCell className="font-mono tabular-nums">{stream.build_duration_seconds.toFixed(3)}s</TableCell></TableRow>
    <TableRow><TableCell>{t("operations.generated_at")}</TableCell><TableCell>{generated}</TableCell></TableRow>
  </TableBody></Table>
}
