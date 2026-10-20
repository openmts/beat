import { useMemo, useState } from "react"
import { PlusIcon } from "lucide-react"

import { DiagnosticsActionDialog, type DiagnosticAction } from "@/components/diagnostics/diagnostics-action-dialog"
import { DiagnosticsCreateDialog } from "@/components/diagnostics/diagnostics-create-dialog"
import { DiagnosticsJobDetail } from "@/components/diagnostics/diagnostics-job-detail"
import { DiagnosticsJobTable, DiagnosticsRefreshButton } from "@/components/diagnostics/diagnostics-job-table"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { useNodes } from "@/hooks/use-api"
import {
  useDiagnosticCapabilities,
  useDiagnosticJobs,
  useDiagnosticPolicy,
  useDiagnosticTargets,
} from "@/hooks/use-diagnostics"
import * as diagnosticsApi from "@/lib/diagnostics-api"
import type { DiagnosticJob, DiagnosticJobFilter, DiagnosticState } from "@/lib/diagnostics-api"
import { diagnosticPreviewInterval, diagnosticStates } from "@/lib/diagnostics-format"

const allValue = "all"

interface PendingAction {
  job: DiagnosticJob
  action: DiagnosticAction
}

export function DiagnosticsPanel() {
  const { t } = useLocale()
  const policy = useDiagnosticPolicy()
  const targets = useDiagnosticTargets()
  const capabilities = useDiagnosticCapabilities(diagnosticPreviewInterval)
  const nodes = useNodes()
  const [filter, setFilter] = useState<DiagnosticJobFilter>({})
  const jobs = useDiagnosticJobs(filter, diagnosticPreviewInterval)
  const [createOpen, setCreateOpen] = useState(false)
  const [detailJobId, setDetailJobId] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const nodeOptions = useMemo(
    () => (nodes.data ?? []).map((node) => ({ id: node.id, name: node.name || node.alias || node.id })),
    [nodes.data],
  )
  const targetOptions = useMemo(
    () => (targets.data ?? []).map((target) => ({ label: target.name, value: target.id })),
    [targets.data],
  )
  const disabled = policy.data !== null && !policy.data.enabled
  const loadError = policy.error ?? targets.error ?? capabilities.error ?? jobs.error ?? nodes.error

  const refreshAll = () => {
    void policy.refresh()
    void targets.refresh()
    void capabilities.refresh()
    void nodes.refresh({ silent: true })
    void jobs.refresh()
  }

  const confirm = async () => {
    if (!pending) return
    setActionLoading(true)
    setActionError(null)
    try {
      if (pending.action === "cancel") await diagnosticsApi.cancelDiagnosticJob(pending.job.id)
      else if (pending.action === "retry") await diagnosticsApi.retryDiagnosticJob(pending.job.id)
      else await diagnosticsApi.deleteDiagnosticJob(pending.job.id)
      setPending(null)
      await jobs.refresh()
    } catch (error) {
      setActionError(actionErrorText(error, t))
    } finally {
      setActionLoading(false)
    }
  }

  if (disabled) {
    return <Alert><AlertDescription>{t("diagnostics.disabled")}</AlertDescription></Alert>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-wrap items-end gap-2">
          <FilterSelect
            label={t("diagnostics.filter_state")}
            value={filter.state || allValue}
            onChange={(value) => setFilter((current) => ({ ...current, state: value === allValue ? "" : (value as DiagnosticState) }))}
            options={diagnosticStates.map((state) => ({ label: t(`diagnostics.state.${state}`), value: state }))}
          />
          <FilterSelect
            label={t("diagnostics.filter_node")}
            value={filter.nodeId || allValue}
            onChange={(value) => setFilter((current) => ({ ...current, nodeId: value === allValue ? "" : value }))}
            options={nodeOptions.map((node) => ({ label: node.name, value: node.id }))}
          />
          <FilterSelect
            label={t("diagnostics.filter_target")}
            value={filter.targetId || allValue}
            onChange={(value) => setFilter((current) => ({ ...current, targetId: value === allValue ? "" : value }))}
            options={targetOptions}
          />
        </div>
        <div className="flex items-center gap-2">
          <DiagnosticsRefreshButton onClick={refreshAll} />
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <PlusIcon data-icon="inline-start" />{t("diagnostics.create")}
          </Button>
        </div>
      </div>
      {loadError && <Alert variant="destructive"><AlertDescription>{loadError}</AlertDescription></Alert>}
      {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
      {policy.loading && policy.data === null ? (
        <div className="flex flex-col gap-2"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
      ) : (
        <DiagnosticsJobTable
          jobs={jobs.data ?? []}
          loading={jobs.loading && jobs.data === null}
          interrupted={jobs.error !== null && jobs.data !== null}
          onSelect={(job) => setDetailJobId(job.id)}
          onCancel={(job) => setPending({ job, action: "cancel" })}
          onRetry={(job) => setPending({ job, action: "retry" })}
          onDelete={(job) => setPending({ job, action: "delete" })}
        />
      )}
      <DiagnosticsCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        policy={policy.data}
        targets={targets.data ?? []}
        capabilities={capabilities.data ?? []}
        nodes={nodeOptions}
        onCreated={async () => { await jobs.refresh() }}
      />
      <DiagnosticsJobDetail jobId={detailJobId} open={detailJobId !== null} onOpenChange={(open) => { if (!open) setDetailJobId(null) }} />
      <DiagnosticsActionDialog
        job={pending?.job ?? null}
        action={pending?.action ?? "cancel"}
        loading={actionLoading}
        onOpenChange={(open) => { if (!open) setPending(null) }}
        onConfirm={() => void confirm()}
      />
    </div>
  )
}

interface FilterSelectProps {
  label: string
  value: string
  onChange: (value: string) => void
  options: { label: string; value: string }[]
}

function FilterSelect(props: FilterSelectProps) {
  const { t } = useLocale()
  const items = useMemo(
    () => [{ label: t("diagnostics.filter_all"), value: allValue }, ...props.options],
    [props.options, t],
  )
  return (
    <label className="flex flex-col gap-1 text-xs text-muted-foreground">
      {props.label}
      <Select items={items} value={props.value} onValueChange={(value) => props.onChange(String(value))}>
        <SelectTrigger className="w-40" aria-label={props.label}><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value={allValue}>{t("diagnostics.filter_all")}</SelectItem>
          {props.options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
        </SelectContent>
      </Select>
    </label>
  )
}

// actionErrorText 把服务端状态码翻译成可执行提示，其余错误原样显示，便于排查。
function actionErrorText(error: unknown, translate: (key: string) => string): string {
  const status = (error as { response?: { status?: number } } | null)?.response?.status
  if (status === 403 || status === 401) return translate("diagnostics.error_forbidden")
  if (status === 409) return translate("diagnostics.error_conflict")
  if (status === 422) return translate("diagnostics.error_capability")
  return error instanceof Error ? error.message : translate("diagnostics.action_failed")
}
