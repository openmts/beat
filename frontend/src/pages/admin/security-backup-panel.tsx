import { useCallback, useEffect, useRef, useState } from "react"
import { ArchiveIcon, DownloadIcon, RefreshCwIcon, RotateCcwIcon, Trash2Icon, UploadIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import {
  createBackup, deleteBackup, downloadBackup, listBackups, stageBackupRestore, validateBackup,
} from "@/lib/backup-api"
import type { BackupRecord } from "@/types/backup"

export function SecurityBackupPanel() {
  const { t } = useLocale()
  const [records, setRecords] = useState<BackupRecord[]>([])
  const [error, setError] = useState("")
  const [message, setMessage] = useState("")
  const [busy, setBusy] = useState(false)
  const [restore, setRestore] = useState<BackupRecord | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    try {
      setRecords(await listBackups())
      setError("")
    } catch (cause) {
      setError(errorMessage(cause, t("app.error")))
    }
  }, [t])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!records.some((record) => record.state === "running")) return
    const timer = window.setInterval(() => void load(), 2000)
    return () => window.clearInterval(timer)
  }, [load, records])

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    setError("")
    setMessage("")
    try {
      await action()
      await load()
    } catch (cause) {
      setError(errorMessage(cause, t("app.error")))
    } finally {
      setBusy(false)
    }
  }

  const upload = (file?: File) => {
    if (file) void run(async () => { await validateBackup(file) })
  }

  return (
    <div className="flex flex-col gap-4">
      {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      {message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
      <Alert><AlertDescription>{t("backup.reauth_hint")}</AlertDescription></Alert>
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="outline" disabled={busy} onClick={() => fileInput.current?.click()}>
          <UploadIcon data-icon="inline-start" />{t("backup.upload")}
        </Button>
        <Input ref={fileInput} className="hidden" type="file" accept="application/zip,.zip"
          onChange={(event) => { upload(event.target.files?.[0]); event.target.value = "" }} />
        <Button disabled={busy || records.some((record) => record.state === "running")}
          onClick={() => void run(async () => { await createBackup() })}>
          <ArchiveIcon data-icon="inline-start" />{t("backup.create")}
        </Button>
      </div>
      {records.length === 0 ? (
        <Card><CardContent className="py-8 text-center text-sm text-muted-foreground">
          {t("backup.empty")}
        </CardContent></Card>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {records.map((record) => (
            <BackupCard key={record.id} record={record} busy={busy} onRestore={setRestore}
              onDownload={() => void run(() => downloadBackup(record))}
              onDelete={() => void run(() => deleteBackup(record.id))} />
          ))}
        </div>
      )}
      <RestoreDialog record={restore} busy={busy} onOpenChange={(open) => !open && setRestore(null)}
        onStage={(confirmation) => void run(async () => {
          if (!restore) return
          await stageBackupRestore(restore.id, confirmation)
          setRestore(null)
          setMessage(t("backup.staged_message"))
        })} />
    </div>
  )
}

function BackupCard({ record, busy, onRestore, onDownload, onDelete }: {
  record: BackupRecord
  busy: boolean
  onRestore: (record: BackupRecord) => void
  onDownload: () => void
  onDelete: () => void
}) {
  const { t } = useLocale()
  const available = record.state === "ready" || record.state === "validated"
  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0"><CardTitle className="truncate text-sm">{record.filename}</CardTitle>
            <CardDescription>{t("backup.created")}: {new Date(record.created_at).toLocaleString()}</CardDescription></div>
          <Badge variant={record.state === "failed" ? "destructive" : "secondary"}>
            {t(`backup.${record.state}`)}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3 text-sm">
          <Metric label={t("backup.size")} value={formatBytes(record.size_bytes)} />
          <Metric label={t("backup.metrics")} value={record.metric_rows.toLocaleString()} />
        </div>
        {record.error_message && <Alert variant="destructive"><AlertDescription>{record.error_message}</AlertDescription></Alert>}
        <div className="flex justify-end gap-1">
          {record.state === "running" && <Button variant="ghost" size="icon-sm" disabled aria-label={t("backup.running")}>
            <RefreshCwIcon className="animate-spin" /></Button>}
          {available && <Button variant="ghost" size="icon-sm" disabled={busy} aria-label={t("backup.download")}
            onClick={onDownload}><DownloadIcon /></Button>}
          {available && <Button variant="ghost" size="icon-sm" disabled={busy} aria-label={t("backup.restore")}
            onClick={() => onRestore(record)}><RotateCcwIcon /></Button>}
          {record.state !== "staged" && record.state !== "running" && <Button variant="ghost" size="icon-sm"
            disabled={busy} aria-label={t("backup.delete")} onClick={onDelete}><Trash2Icon /></Button>}
        </div>
      </CardContent>
    </Card>
  )
}

function RestoreDialog({ record, busy, onOpenChange, onStage }: {
  record: BackupRecord | null
  busy: boolean
  onOpenChange: (open: boolean) => void
  onStage: (confirmation: string) => void
}) {
  const { t } = useLocale()
  const [confirmation, setConfirmation] = useState("")
  useEffect(() => setConfirmation(""), [record?.id])
  return <Dialog open={record !== null} onOpenChange={onOpenChange}>
    <DialogContent><DialogHeader><DialogTitle>{t("backup.restore_title")}</DialogTitle>
      <DialogDescription>{t("backup.restore_description")}</DialogDescription></DialogHeader>
      <Field><FieldLabel htmlFor="restore-confirmation">{t("backup.confirmation")}</FieldLabel>
        <Input id="restore-confirmation" value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)} />
        <FieldDescription>{record?.filename}</FieldDescription></Field>
      <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
        <Button variant="destructive" disabled={busy || confirmation !== "RESTORE BEAT"}
          onClick={() => onStage(confirmation)}><RotateCcwIcon data-icon="inline-start" />{t("backup.stage")}</Button>
      </DialogFooter></DialogContent>
  </Dialog>
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><p className="text-xs text-muted-foreground">{label}</p><p className="font-medium">{value}</p></div>
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback
}
