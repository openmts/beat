import { CircleAlertIcon, CircleCheckIcon, ClockIcon, DownloadIcon, LoaderIcon, Trash2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import {
  formatRuntimeBytes,
  runtimeFailureLabelKeys,
  runtimeKindLabelKeys,
  runtimeLabel,
  runtimeStatusLabelKeys,
} from "@/lib/runtime-diagnostics-format"
import type { RuntimeCapture } from "@/types/runtime-diagnostics"

const statusVariants: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  running: "secondary",
  complete: "default",
  failed: "destructive",
  cancelled: "outline",
  expired: "outline",
}

// 状态始终是图标加文字，颜色只是补充，屏幕阅读器先读到文字。
const statusIcons: Record<string, typeof ClockIcon> = {
  running: LoaderIcon,
  complete: CircleCheckIcon,
  failed: CircleAlertIcon,
  cancelled: CircleAlertIcon,
  expired: ClockIcon,
}

function captureTimestamp(value: string, locale: string) {
  const at = Date.parse(value)
  return Number.isFinite(at) && at > 0 ? formatDateTime(locale, at) : "—"
}

function CaptureActions({ capture, busy, onDownload, onRemove }: {
  capture: RuntimeCapture
  busy: boolean
  onDownload: (capture: RuntimeCapture) => void
  onRemove: (capture: RuntimeCapture) => void
}) {
  const { t } = useLocale()
  const downloadable = capture.status === "complete"
  return (
    <div className="flex justify-end gap-1">
      {downloadable ? (
        <Button size="sm" variant="outline" disabled={busy} onClick={() => onDownload(capture)}>
          <DownloadIcon aria-hidden />{t("runtime.download")}
        </Button>
      ) : null}
      <Button size="sm" variant="ghost" disabled={busy} onClick={() => onRemove(capture)}>
        <Trash2Icon aria-hidden />{capture.status === "running" ? (t("runtime.cancel")) : t("runtime.delete")}
      </Button>
    </div>
  )
}

function StatusBadge({ capture }: { capture: RuntimeCapture }) {
  const { t } = useLocale()
  const Icon = statusIcons[capture.status] ?? ClockIcon
  return <Badge variant={statusVariants[capture.status] ?? "outline"}><Icon aria-hidden />{runtimeLabel(t, runtimeStatusLabelKeys, capture.status)}</Badge>
}

function CaptureRow({ capture, busy, onDownload, onRemove }: {
  capture: RuntimeCapture
  busy: boolean
  onDownload: (capture: RuntimeCapture) => void
  onRemove: (capture: RuntimeCapture) => void
}) {
  const { t, locale } = useLocale()
  const failure = capture.failure_code ? runtimeLabel(t, runtimeFailureLabelKeys, capture.failure_code) : ""
  return (
    <TableRow>
      <TableCell className="font-medium">{runtimeLabel(t, runtimeKindLabelKeys, capture.kind)}</TableCell>
      <TableCell>{captureTimestamp(capture.started_at, locale)}</TableCell>
      <TableCell>
        <StatusBadge capture={capture} />
        {failure ? <p className="mt-1 text-xs text-muted-foreground">{failure}</p> : null}
      </TableCell>
      <TableCell>{capture.status === "complete" ? formatRuntimeBytes(capture.bytes) : "—"}</TableCell>
      <TableCell>{captureTimestamp(capture.expires_at, locale)}</TableCell>
      <TableCell>
        <CaptureActions capture={capture} busy={busy} onDownload={onDownload} onRemove={onRemove} />
      </TableCell>
    </TableRow>
  )
}

function CaptureCard({ capture, busy, onDownload, onRemove }: {
  capture: RuntimeCapture
  busy: boolean
  onDownload: (capture: RuntimeCapture) => void
  onRemove: (capture: RuntimeCapture) => void
}) {
  const { t, locale } = useLocale()
  return (
    <div className="space-y-2 rounded-md border p-3">
      <div className="flex items-start justify-between gap-2">
        <p className="font-medium">{runtimeLabel(t, runtimeKindLabelKeys, capture.kind)}</p>
        <StatusBadge capture={capture} />
      </div>
      <p className="text-xs text-muted-foreground">
        {captureTimestamp(capture.started_at, locale)} · {capture.status === "complete" ? formatRuntimeBytes(capture.bytes) : "—"}
      </p>
      {capture.failure_code ? <p className="text-xs text-muted-foreground">{runtimeLabel(t, runtimeFailureLabelKeys, capture.failure_code)}</p> : null}
      <CaptureActions capture={capture} busy={busy} onDownload={onDownload} onRemove={onRemove} />
    </div>
  )
}

export function RuntimeDiagnosticsTable({ captures, loading, busy, onDownload, onRemove }: {
  captures: RuntimeCapture[]
  loading: boolean
  busy: boolean
  onDownload: (capture: RuntimeCapture) => void
  onRemove: (capture: RuntimeCapture) => void
}) {
  const { t } = useLocale()
  if (!captures.length) {
    return <p className="p-8 text-center text-sm text-muted-foreground">{loading ? (t("runtime.loading")) : t("runtime.no_diagnostic_artifacts")}</p>
  }
  return (
    <>
      <div className="space-y-3 sm:hidden">
        {captures.map((capture) => (
          <CaptureCard key={capture.id} capture={capture} busy={busy} onDownload={onDownload} onRemove={onRemove} />
        ))}
      </div>
      <div className="hidden overflow-auto sm:block">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("runtime.kind")}</TableHead>
              <TableHead>{t("runtime.started")}</TableHead>
              <TableHead>{t("runtime.status")}</TableHead>
              <TableHead>{t("runtime.size")}</TableHead>
              <TableHead>{t("runtime.expires")}</TableHead>
              <TableHead className="text-right">{t("runtime.actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {captures.map((capture) => (
              <CaptureRow key={capture.id} capture={capture} busy={busy} onDownload={onDownload} onRemove={onRemove} />
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}
