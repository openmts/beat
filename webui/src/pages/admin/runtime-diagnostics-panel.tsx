import { useCallback, useState } from "react"
import { ActivityIcon, MinusIcon, PlusIcon } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ReauthDialog } from "@/components/reauth-dialog"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useReauthRetry } from "@/hooks/use-reauth-retry"
import { cancelRuntimeCapture, downloadRuntimeCapture, getRuntimeDiagnosticsSummary, listRuntimeCaptures, saveCapturedProfile, startRuntimeCapture } from "@/lib/runtime-diagnostics-api"
import { clampRuntimeDuration, runtimeCaptureKinds, runtimeDurationBounds, runtimeKindIsTimed, runtimeKindLabelKeys, runtimeLabel, runtimePollMilliseconds } from "@/lib/runtime-diagnostics-format"
import type { RuntimeCapture, RuntimeCaptureKind } from "@/types/runtime-diagnostics"
import { RuntimeDiagnosticsSummary } from "./runtime-diagnostics-summary"
import { RuntimeDiagnosticsTable } from "./runtime-diagnostics-table"

type Confirmation = { title: string; description: string; confirmLabel: string; action: () => Promise<unknown> }

function failureMessage(error: unknown) {
  return error instanceof Error ? error.message : "Request failed"
}

export function RuntimeDiagnosticsPanel() {
  const { t } = useLocale()
  const summary = useOperationResource(getRuntimeDiagnosticsSummary, runtimePollMilliseconds)
  const captures = useOperationResource(listRuntimeCaptures, runtimePollMilliseconds)
  const [kind, setKind] = useState<RuntimeCaptureKind>("cpu")
  const [duration, setDuration] = useState(10)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState("")
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const bounds = runtimeDurationBounds(kind)
  const timed = runtimeKindIsTimed(kind)
  const reauth = useReauthRetry()

  const run = useCallback(async (action: () => Promise<unknown>) => {
    setPending(true)
    setError("")
    try {
      // 采集与下载要求近期重认证：428 时整体挂起，确认身份后连同刷新一起重放。
      await reauth.run(async () => {
        await action()
        captures.reload()
        summary.reload()
      })
    } catch (failure) {
      setError(failureMessage(failure))
    } finally {
      setPending(false)
      setConfirmation(null)
    }
  }, [captures, reauth, summary])

  const requestStart = useCallback(() => {
    const seconds = timed ? clampRuntimeDuration(kind, duration) : 0
    const action = () => startRuntimeCapture({ kind, duration_seconds: seconds })
    const label = runtimeLabel(t, runtimeKindLabelKeys, kind)
    if (!timed) {
      void run(action)
      return
    }
    setConfirmation({
      title: t("runtime.start_kind", { kind: label }),
      description: t("runtime.sampling_description", { seconds }),
      confirmLabel: t("runtime.start_capture"),
      action,
    })
  }, [duration, kind, run, t, timed])

  const requestRemove = useCallback((capture: RuntimeCapture) => {
    const label = runtimeLabel(t, runtimeKindLabelKeys, capture.kind)
    setConfirmation({
      title: capture.status === "running" ? t("runtime.cancel_kind", { kind: label }) : t("runtime.delete_kind", { kind: label }),
      description: capture.status === "running"
        ? t("runtime.cancelling_stops_sampling_and_deletes_the_writte")
        : t("runtime.the_artifact_is_removed_you_would_need_a_new_cap"),
      confirmLabel: capture.status === "running" ? (t("runtime.cancel_capture")) : t("runtime.delete"),
      action: () => cancelRuntimeCapture(capture.id),
    })
  }, [t])

  const download = useCallback((capture: RuntimeCapture) => {
    void run(async () => saveCapturedProfile(capture, await downloadRuntimeCapture(capture.id)))
  }, [run])

  return (
    <div className="space-y-4">
      <Alert>
        <ActivityIcon aria-hidden />
        <AlertTitle>{t("runtime.runtime_diagnostics")}</AlertTitle>
        <AlertDescription>
          {t("runtime.profiles_can_contain_sensitive_runtime_detail_th")}
        </AlertDescription>
      </Alert>
      <RuntimeDiagnosticsSummary summary={summary.data} />
      <Card>
        <CardHeader><CardTitle>{t("runtime.new_capture")}</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <FieldGroup className="sm:flex-row sm:items-end">
            <Field>
              <FieldLabel htmlFor="runtime-capture-kind">{t("runtime.capture_kind")}</FieldLabel>
              <Select
                items={runtimeCaptureKinds.map((value) => ({ value, label: runtimeLabel(t, runtimeKindLabelKeys, value) }))}
                value={kind}
                onValueChange={(value) => { const next = String(value ?? "cpu") as RuntimeCaptureKind; setKind(next); setDuration(clampRuntimeDuration(next, duration)) }}
              >
                <SelectTrigger id="runtime-capture-kind"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {runtimeCaptureKinds.map((value) => <SelectItem key={value} value={value}>{runtimeLabel(t, runtimeKindLabelKeys, value)}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
            {timed ? (
              <Field>
                <FieldLabel htmlFor="runtime-capture-duration">{t("runtime.duration_seconds")}</FieldLabel>
                <div className="flex items-center gap-2">
                  <Button type="button" size="sm" variant="outline" aria-label={t("runtime.decrease_duration")} disabled={pending || duration <= bounds.min} onClick={() => setDuration((value) => clampRuntimeDuration(kind, value - 1))}><MinusIcon aria-hidden /></Button>
                  <input
                    id="runtime-capture-duration"
                    className="h-9 w-20 rounded-md border bg-transparent px-2 text-center text-sm"
                    type="number"
                    min={bounds.min}
                    max={bounds.max}
                    value={duration}
                    onChange={(event) => setDuration(clampRuntimeDuration(kind, Number(event.target.value)))}
                  />
                  <Button type="button" size="sm" variant="outline" aria-label={t("runtime.increase_duration")} disabled={pending || duration >= bounds.max} onClick={() => setDuration((value) => clampRuntimeDuration(kind, value + 1))}><PlusIcon aria-hidden /></Button>
                </div>
                <FieldDescription>{t("runtime.bounded_duration", { min: bounds.min, max: bounds.max })}</FieldDescription>
              </Field>
            ) : (
              <Field>
                <FieldDescription>{t("runtime.instantaneous_profiles_write_once_and_are_immedi")}</FieldDescription>
              </Field>
            )}
            <Field>
              <Button type="button" disabled={pending} onClick={requestStart}><ActivityIcon aria-hidden />{t("runtime.start_capture")}</Button>
            </Field>
          </FieldGroup>
          {error ? <Alert variant="destructive"><AlertDescription role="alert">{error}</AlertDescription></Alert> : null}
          {captures.error ? <p role="alert" className="text-sm text-destructive">{captures.error}</p> : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{t("runtime.artifacts")}</CardTitle></CardHeader>
        <CardContent>
          {reauth.pending && <ReauthDialog codeRequired={reauth.codeRequired} pending={reauth.busy} error={reauth.error}
        onClose={reauth.close} onConfirm={(password, code) => void reauth.confirm(password, code)} />}
      <RuntimeDiagnosticsTable
            captures={captures.data?.captures ?? []}
            loading={captures.loading}
            busy={pending}
            onDownload={download}
            onRemove={requestRemove}
          />
        </CardContent>
      </Card>
      <AlertDialog open={Boolean(confirmation)} onOpenChange={(open) => { if (!open) setConfirmation(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmation?.title ?? ""}</AlertDialogTitle>
            <AlertDialogDescription>{confirmation?.description ?? ""}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>{t("runtime.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={pending} onClick={() => { if (confirmation) void run(confirmation.action) }}>{confirmation?.confirmLabel ?? (t("runtime.confirm"))}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
