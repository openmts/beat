import { useEffect, useState } from "react"

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
import { Alert, AlertDescription } from "@/components/ui/alert"
import { EraseBackupNotice } from "@/components/erase-backup-notice"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { useBackupReadiness } from "@/hooks/use-backup-readiness"
import {
  erasureErasesEntity,
  erasurePhrase,
  newMetricErasureRequestID,
  type MetricErasureScope,
} from "@/lib/metric-erasure-api"

export interface MetricErasureDraft {
  scope: MetricErasureScope
  targetId: string
  targetName: string
}

/** 确认对话框明确写出受影响的测量类别、实体去留、恢复证据与唯一回退路径。 */
export function MetricErasureDialog(props: {
  open: boolean
  draft: MetricErasureDraft | null
  pending: boolean
  error: string | null
  onOpenChange: (open: boolean) => void
  onSubmit: (confirmation: string, requestId: string) => Promise<void>
}) {
  const { t } = useLocale()
  const draft = props.draft
  const [typed, setTyped] = useState("")
  const [acknowledged, setAcknowledged] = useState(false)
  const [requestId, setRequestId] = useState(() => newMetricErasureRequestID())
  const readiness = useBackupReadiness(props.open)

  useEffect(() => {
    setTyped("")
    setAcknowledged(false)
    setRequestId(newMetricErasureRequestID())
  }, [draft])

  if (!props.open || !draft) return null
  const phrase = erasurePhrase(draft.scope)
  const expected = phrase !== "" ? phrase : draft.targetName
  const confirmed = typed.trim() === expected && (Boolean(readiness.ready) || acknowledged)
  return (
    <AlertDialog open onOpenChange={(open) => { if (!open) props.onOpenChange(false) }}>
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("confirm.delete")}</AlertDialogTitle>
          <AlertDialogDescription>{t(`erase.affected.${draft.scope}`)}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            {t(erasureErasesEntity(draft.scope) ? "erase.entity_removed" : "erase.entity_kept")}
          </p>
          <EraseBackupNotice
            open={props.open}
            readiness={readiness}
            acknowledged={acknowledged}
            onAcknowledgeChange={setAcknowledged}
          />
          <Field>
            <FieldLabel htmlFor="metric-erasure-confirmation">
              {t(phrase !== "" ? "erase.phrase_label" : "erase.name_label")}
            </FieldLabel>
            <Input
              id="metric-erasure-confirmation"
              value={typed}
              disabled={props.pending}
              placeholder={expected}
              onChange={(event) => setTyped(event.target.value)}
            />
            <FieldDescription>
              {t(phrase !== "" ? "erase.phrase_hint" : "erase.name_hint")}
            </FieldDescription>
          </Field>
          {props.error ? (
            <Alert variant="destructive"><AlertDescription>{props.error}</AlertDescription></Alert>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>{t("app.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            disabled={props.pending || !confirmed}
            onClick={() => void props.onSubmit(typed.trim(), requestId)}
          >
            {t("erase.confirm")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
