import { useState } from "react"
import { DatabaseBackupIcon } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { useLocale } from "@/context/locale"
import type { BackupReadiness } from "@/hooks/use-backup-readiness"

/**
 * 擦除前必须看到的恢复证据。没有近期就绪备份时，这里只提供显式创建入口，
 * 并要求单独的确认勾选，绝不静默生成可能很大的备份。
 */
export function EraseBackupNotice(props: {
  open: boolean
  readiness: BackupReadiness
  acknowledged: boolean
  onAcknowledgeChange: (value: boolean) => void
}) {
  const { t } = useLocale()
  const [error, setError] = useState<string | null>(null)
  const readiness = props.readiness
  const run = async () => {
    setError(null)
    try {
      await readiness.create()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : t("erase.error"))
    }
  }
  return (
    <div className="space-y-3">
      <Alert>
        <AlertDescription>{describe(t, readiness)}</AlertDescription>
      </Alert>
      <p className="text-xs text-muted-foreground">{t("erase.recovery_hint")}</p>
      {!readiness.loading && !readiness.ready ? (
        <div className="flex flex-wrap items-start gap-3">
          <Button type="button" variant="outline" size="sm" disabled={readiness.creating} onClick={() => void run()}>
            <DatabaseBackupIcon data-icon="inline-start" />
            {t(readiness.creating ? "erase.backup_creating" : "erase.backup_create")}
          </Button>
          <label className="flex max-w-md items-start gap-2 text-sm">
            <Checkbox
              checked={props.acknowledged}
              disabled={readiness.creating}
              onCheckedChange={(value) => props.onAcknowledgeChange(Boolean(value))}
            />
            {t("erase.ack_backup")}
          </label>
        </div>
      ) : null}
      {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    </div>
  )
}

function describe(t: ReturnType<typeof useLocale>["t"], readiness: BackupReadiness): string {
  if (readiness.loading) return t("erase.loading_backup")
  if (readiness.ready) return t("erase.backup_ready", { name: readiness.ready.filename })
  if (readiness.unknown) return t("erase.backup_unknown")
  return t("erase.backup_missing")
}
