import { useMemo, useState } from "react"
import { EraserIcon } from "lucide-react"

import { ReauthDialog } from "@/components/reauth-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  type SelectOption,
} from "@/components/ui/select"
import { useLocale } from "@/context/locale"
import { useNodes } from "@/hooks/use-api"
import { useAdminNetworkTasks } from "@/hooks/use-network"
import { erasureTargetKind, metricErasureScopes, type MetricErasureScope } from "@/lib/metric-erasure-api"
import { MetricErasureDialog, type MetricErasureDraft } from "@/pages/admin/metric-erasure-dialog"
import { MetricErasureJobs } from "@/pages/admin/metric-erasure-jobs"
import { useErasureState, type ErasureState, type JobAction } from "@/pages/admin/metric-erasure-state"

/** 擦除面板只接受封闭范围枚举与既有实体 ID；界面永远显示名字而不是原始 ID。 */
export function MetricErasurePanel() {
  const { t } = useLocale()
  const nodes = useNodes()
  const tasks = useAdminNetworkTasks()
  const state = useErasureState()
  const [draft, setDraft] = useState<MetricErasureDraft | null>(null)
  const [action, setAction] = useState<JobAction | null>(null)

  const kind = erasureTargetKind(state.scope)
  const entityOptions = useMemo<SelectOption[]>(() => {
    if (kind === "node") return (nodes.data ?? []).map((node) => ({ value: node.id, label: node.name }))
    if (kind === "task") return (tasks.data ?? []).map((view) => ({ value: view.task.id, label: view.task.name }))
    return []
  }, [kind, nodes.data, tasks.data])
  const selected = entityOptions.find((option) => option.value === state.targetId)
  const selectedName = typeof selected?.label === "string" ? selected.label : ""

  return (
    <div className="space-y-5">
      <div>
        <h3 className="text-sm font-semibold">{t("erase.title")}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{t("erase.description")}</p>
      </div>

      {state.error && !draft ? (
        <Alert variant="destructive"><AlertDescription>{state.error}</AlertDescription></Alert>
      ) : null}
      {state.message ? <Alert><AlertDescription>{state.message}</AlertDescription></Alert> : null}

      <ErasureForm
        form={state}
        entityOptions={entityOptions}
        kind={kind}
        onConfirm={() => setDraft({
          scope: state.scope, targetId: state.targetId,
          targetName: kind === "none" ? "" : selectedName,
        })}
      />

      <ErasureJobSection state={state} onAction={setAction} />

      <MetricErasureDialog
        open={draft !== null}
        draft={draft}
        pending={state.pending}
        error={state.error}
        onOpenChange={(open) => { if (!open) setDraft(null) }}
        onSubmit={state.submit}
      />
      <JobActionDialog
        action={action}
        pending={state.pending}
        onOpenChange={(open) => { if (!open) setAction(null) }}
        onConfirm={() => void state.runAction(action, () => setAction(null))}
      />
      {state.reauth.pending ? (
        <ReauthDialog
          codeRequired={state.reauth.codeRequired}
          pending={state.reauth.busy}
          error={state.reauth.error}
          onClose={state.reauth.close}
          onConfirm={(password, code) => void state.reauth.confirm(password, code)}
        />
      ) : null}
    </div>
  )
}

function ErasureForm(props: {
  form: ErasureState
  kind: "none" | "node" | "task"
  entityOptions: SelectOption[]
  onConfirm: () => void
}) {
  const { t } = useLocale()
  const form = props.form
  const scopeOptions = useMemo<SelectOption[]>(
    () => metricErasureScopes.map((item) => ({ value: item, label: t(`erase.scope.${item}`) })),
    [t],
  )
  const ready = props.kind === "none" || form.targetId !== ""
  return (
    <div className="space-y-5">
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="erasure-scope">{t("erase.scope")}</FieldLabel>
          <Select
            items={scopeOptions}
            value={form.scope}
            disabled={form.pending}
            onValueChange={(value) => form.setScope(value as MetricErasureScope)}
          >
            <SelectTrigger id="erasure-scope" className="w-full max-w-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {scopeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <FieldDescription>{t(`erase.affected.${form.scope}`)}</FieldDescription>
        </Field>

        {props.kind !== "none" ? (
          <Field>
            <FieldLabel htmlFor="erasure-target">{t("erase.target")}</FieldLabel>
            <Select
              items={props.entityOptions}
              value={form.targetId}
              disabled={form.pending || props.entityOptions.length === 0}
              onValueChange={(value) => form.setTargetId(value ?? "")}
            >
              <SelectTrigger id="erasure-target" className="w-full max-w-sm">
                <SelectValue placeholder={t("erase.target_placeholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {props.entityOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {props.entityOptions.length === 0 ? (
              <FieldDescription>{t("erase.no_targets")}</FieldDescription>
            ) : null}
          </Field>
        ) : null}
      </FieldGroup>

      <div className="flex justify-end border-t pt-4">
        <Button variant="destructive" disabled={form.pending || !ready} onClick={props.onConfirm}>
          <EraserIcon data-icon="inline-start" />{t("erase.submit")}
        </Button>
      </div>
    </div>
  )
}

function ErasureJobSection(props: { state: ErasureState; onAction: (action: JobAction) => void }) {
  const { t } = useLocale()
  return (
    <div>
      <h3 className="mb-3 text-sm font-semibold">{t("erase.table_scope")}</h3>
      {props.state.loading ? <p className="text-sm text-muted-foreground">{t("erase.loading")}</p> : (
        <MetricErasureJobs
          jobs={props.state.jobs}
          pending={props.state.pending}
          onCancel={(job) => props.onAction({ job, kind: "cancel" })}
          onRetry={(job) => props.onAction({ job, kind: "retry" })}
        />
      )}
    </div>
  )
}

function JobActionDialog(props: {
  action: JobAction | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  const { t } = useLocale()
  return (
    <AlertDialog open={props.action !== null} onOpenChange={props.onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(props.action?.kind === "cancel" ? "erase.cancel_confirm_title" : "erase.retry_confirm_title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(props.action?.kind === "cancel" ? "erase.cancel_confirm_body" : "erase.retry_confirm_body")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>{t("app.cancel")}</AlertDialogCancel>
          <AlertDialogAction disabled={props.pending} onClick={props.onConfirm}>
            {t(props.action?.kind === "cancel" ? "erase.cancel" : "erase.retry")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export default MetricErasurePanel
