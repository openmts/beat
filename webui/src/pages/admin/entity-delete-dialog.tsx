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
  newMetricErasureRequestID,
  type MetricErasureScope,
} from "@/lib/metric-erasure-api"

export interface EntityDeleteTarget {
  id: string
  name: string
  scope: MetricErasureScope
}

/**
 * 节点与质量任务共用同一个删除对话框：删除实际上是“擦除历史 + 移除实体”的
 * 耐久任务，所以确认值必须是精确显示名，并且必须先看到恢复证据。
 */
export function EntityDeleteDialog(props: {
  target: EntityDeleteTarget | null
  pending: boolean
  error: string | null
  onOpenChange: (open: boolean) => void
  onConfirm: (requestId: string) => Promise<void>
}) {
  const { t } = useLocale()
  const [typed, setTyped] = useState("")
  const [acknowledged, setAcknowledged] = useState(false)
  const [requestId, setRequestId] = useState(() => newMetricErasureRequestID())
  const readiness = useBackupReadiness(props.target !== null)
  const target = props.target

  useEffect(() => {
    if (!target) return
    setTyped("")
    setAcknowledged(false)
    setRequestId(newMetricErasureRequestID())
  }, [target])

  if (!target) return null
  const confirmed = typed.trim() === target.name && (Boolean(readiness.ready) || acknowledged)
  return (
    <AlertDialog open onOpenChange={(open) => { if (!open) props.onOpenChange(false) }}>
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("confirm.delete")}</AlertDialogTitle>
          <AlertDialogDescription>{t(`erase.affected.${target.scope}`)}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            {t(erasureErasesEntity(target.scope) ? "erase.entity_removed" : "erase.entity_kept")}
          </p>
          <EraseBackupNotice
            open
            readiness={readiness}
            acknowledged={acknowledged}
            onAcknowledgeChange={setAcknowledged}
          />
          <Field>
            <FieldLabel htmlFor="entity-delete-name">{t("erase.name_label")}</FieldLabel>
            <Input
              id="entity-delete-name"
              value={typed}
              disabled={props.pending}
              placeholder={target.name}
              onChange={(event) => setTyped(event.target.value)}
            />
            <FieldDescription>{t("erase.name_hint")}</FieldDescription>
          </Field>
          {props.error ? (
            <Alert variant="destructive"><AlertDescription>{props.error}</AlertDescription></Alert>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>{t("app.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            disabled={props.pending || !confirmed}
            onClick={() => void props.onConfirm(requestId)}
          >
            {t("app.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
