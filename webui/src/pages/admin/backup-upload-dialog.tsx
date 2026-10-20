import { useCallback, useEffect, useRef, useState } from "react"
import { UploadIcon, XIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { ReauthDialog } from "@/components/reauth-dialog"
import { useLocale } from "@/context/locale"
import { useReauthRetry } from "@/hooks/use-reauth-retry"
import { cancelBackupUpload, listBackupUploads } from "@/lib/backup-api"
import { resumableBackupUpload, type BackupUploadProgress } from "@/lib/backup-upload"
import type { BackupRecord, BackupUploadSession } from "@/types/backup"

export function BackupUploadDialog({ open, onOpenChange, onUploaded }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onUploaded: (record: BackupRecord) => void
}) {
  const { t } = useLocale()
  const [file, setFile] = useState<File | null>(null)
  const [sessions, setSessions] = useState<BackupUploadSession[]>([])
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [progress, setProgress] = useState<BackupUploadProgress | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const reauth = useReauthRetry()

  const release = useCallback(() => {
    controller.current?.abort()
    controller.current = null
    setFile(null)
    setSessionId(null)
    setProgress(null)
    setError("")
    setBusy(false)
  }, [])

  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => { if (!open) release() }, [open, release])
  useEffect(() => {
    if (!open) return
    let active = true
    void listBackupUploads()
      .then((list) => { if (active) setSessions(list.filter((item) => item.state === "open")) })
      .catch(() => { if (active) setSessions([]) })
    return () => { active = false }
  }, [open])

  const resumable = file
    ? sessions.find((item) => item.filename === file.name && item.declared_bytes === file.size)
    : undefined

  const start = async () => {
    if (!file) return
    const abort = new AbortController()
    controller.current = abort
    setBusy(true)
    setError("")
    try {
      // 上传会话的建立要求近期重认证；428 时把「上传 + 收尾」整体挂起，确认身份后重放，
      // 命中同一会话时上传本身也会续传。
      await reauth.run(async () => {
        const record = await resumableBackupUpload(file, {
          signal: abort.signal,
          resumeSessionId: resumable?.id,
          onProgress: setProgress,
          onSession: (session) => setSessionId(session.id),
        })
        release()
        onUploaded(record)
      })
    } catch (cause) {
      setError(abort.signal.aborted
        ? t("backup.upload_interrupted")
        : cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      controller.current = null
      setBusy(false)
    }
  }

  const discard = async () => {
    const pending = sessionId
    controller.current?.abort()
    controller.current = null
    setBusy(false)
    setProgress(null)
    setSessionId(null)
    if (!pending) return
    try {
      await cancelBackupUpload(pending)
      setSessions((list) => list.filter((item) => item.id !== pending))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const percent = progress && progress.totalBytes > 0
    ? Math.round((progress.uploadedBytes / progress.totalBytes) * 100)
    : 0

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) release(); onOpenChange(next) }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("backup.upload_title")}</DialogTitle>
          <DialogDescription>{t("backup.upload_description")}</DialogDescription>
        </DialogHeader>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        <Field>
          <FieldLabel htmlFor="backup-upload-file">{t("backup.upload_file")}</FieldLabel>
          <Input id="backup-upload-file" type="file" accept="application/zip,.zip" disabled={busy}
            onChange={(event) => { setFile(event.target.files?.[0] ?? null); setError(""); setProgress(null) }} />
          <FieldDescription>{resumable
            ? t("backup.upload_resume_hint")
            : t("backup.upload_new_hint")}</FieldDescription>
        </Field>
        {file && (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="truncate">{file.name}</span>
            <Badge variant="outline">{formatBytes(file.size)}</Badge>
            {resumable && <Badge variant="secondary">{t("backup.upload_resumable")}</Badge>}
          </div>
        )}
        {progress && (
          <div role="status" className="flex flex-col gap-1 text-sm">
            <span>{t(`backup.phase_${progress.phase}`)}{" "}
              {progress.phase === "uploading"
                ? `${progress.completedChunks}/${progress.totalChunks}`
                : `${percent}%`}</span>
            <Progress value={percent} aria-label={`${t("backup.upload_title")} ${percent}%`} />
          </div>
        )}
        <DialogFooter>
          {busy && <Button variant="outline" onClick={() => void discard()}>
            <XIcon data-icon="inline-start" />{t("backup.upload_cancel")}
          </Button>}
          <Button variant="outline" onClick={() => { release(); onOpenChange(false) }}>
            {t("app.cancel")}
          </Button>
          <Button disabled={busy || !file} onClick={() => void start()}>
            <UploadIcon data-icon="inline-start" />{resumable
              ? t("backup.upload_resume")
              : t("backup.upload_start")}
          </Button>
        </DialogFooter>
      </DialogContent>
      {reauth.pending && <ReauthDialog codeRequired={reauth.codeRequired} pending={reauth.busy} error={reauth.error}
        onClose={reauth.close} onConfirm={(password, code) => void reauth.confirm(password, code)} />}
    </Dialog>
  )
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}
