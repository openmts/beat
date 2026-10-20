import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useLocale } from "@/context/locale"
import { formatRuntimeBytes, formatRuntimePause, formatRuntimeUptime } from "@/lib/runtime-diagnostics-format"
import type { RuntimeSummary } from "@/types/runtime-diagnostics"

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-md border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 truncate text-sm font-medium" title={value}>
        {value}
      </p>
      {hint ? <p className="mt-1 truncate text-xs text-muted-foreground" title={hint}>{hint}</p> : null}
    </div>
  )
}

export function RuntimeDiagnosticsSummary({ summary }: { summary?: RuntimeSummary }) {
  const { t } = useLocale()
  if (!summary) return <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">{Array.from({ length: 8 }, (_, index) => <Skeleton key={index} className="h-[68px] w-full" />)}</div>
  const build = `${summary.version || "unknown"} · ${summary.commit || "unknown"}`
  const state = summary.capture.degraded
    ? { label: t("runtime.degraded"), variant: "destructive" as const }
    : summary.readiness === "ok"
      ? { label: t("runtime.ready"), variant: "secondary" as const }
      : { label: t("runtime.attention"), variant: "destructive" as const }
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium">{t("runtime.process_state")}</span>
        <Badge variant={state.variant}>{state.label}</Badge>
        <span className="text-muted-foreground">
          {summary.capture.active > 0 ? t("runtime.capture_in_progress", { count: summary.capture.active }) : t("runtime.no_capture_running")}
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Tooltip>
          <TooltipTrigger render={<div className="text-left" />}>
            <Metric label={t("runtime.build")} value={build} hint={summary.go_version} />
          </TooltipTrigger>
          <TooltipContent>{build}</TooltipContent>
        </Tooltip>
        <Metric label={t("runtime.uptime")} value={formatRuntimeUptime(summary.uptime_seconds, t)} />
        <Metric label={t("runtime.heap")} value={formatRuntimeBytes(summary.memory.heap_alloc_bytes)} hint={`${t("runtime.system")} ${formatRuntimeBytes(summary.memory.system_bytes)}`} />
        <Metric label={t("runtime.goroutines_threads")} value={`${summary.scheduler.goroutines} / ${summary.scheduler.os_threads}`} hint={`GOMAXPROCS ${summary.scheduler.gomaxprocs}`} />
        <Metric label={t("runtime.gc_cycles")} value={String(summary.gc.cycles)} hint={`${t("runtime.last_pause")} ${formatRuntimePause(summary.gc.last_pause_seconds)}`} />
        <Metric label={t("runtime.gc_pause_total")} value={formatRuntimePause(summary.gc.pause_total_seconds)} />
        <Metric label={t("runtime.stack")} value={formatRuntimeBytes(summary.memory.stack_inuse_bytes)} />
        <Metric label={t("runtime.cgo_calls")} value={String(summary.scheduler.cgo_calls)} />
      </div>
    </div>
  )
}
