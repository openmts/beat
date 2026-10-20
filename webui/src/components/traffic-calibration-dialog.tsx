import { useMemo, useState } from "react"
import { GaugeIcon } from "lucide-react"

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useLocale } from "@/context/locale"
import { formatBytes } from "@/lib/metric-format"
import {
  formatCalibrationBytes, newCalibrationRequestID, parseCalibrationBytes,
  type TrafficCalibrationPayload,
} from "@/lib/traffic-calibration"
import type { ManagedNode } from "@/types"

export type TrafficCalibrationMode = "set" | "reset"

interface TrafficCalibrationDialogProps {
  node: ManagedNode | null
  open: boolean
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (payload: TrafficCalibrationPayload, mode: TrafficCalibrationMode) => void
  onOpenHistory: () => void
}

const maximumReasonLength = 500

export function TrafficCalibrationDialog(props: TrafficCalibrationDialogProps) {
  const { t } = useLocale()
  const node = props.node
  const [mode, setMode] = useState<TrafficCalibrationMode>("set")
  const [upload, setUpload] = useState("")
  const [download, setDownload] = useState("")
  const [reason, setReason] = useState("")
  const [effective, setEffective] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState("")
  const [requestId] = useState(() => newCalibrationRequestID())

  const draft = useMemo(() => buildDraft({ mode, upload, download, reason, effective }), [mode, upload, download, reason, effective])
  const validationMessage = messageFor(draft.code, t)

  const reset = () => {
    setMode("set")
    setUpload("")
    setDownload("")
    setReason("")
    setEffective("")
    setConfirming(false)
    setError("")
  }

  if (!node) return null

  const close = (open: boolean) => {
    if (!open) reset()
    props.onOpenChange(open)
  }

  const submit = () => {
    if (draft.code) { setError(validationMessage); return }
    setError("")
    setConfirming(true)
  }

  if (confirming) {
    return (
      <AlertDialog open onOpenChange={(open) => { if (!open) setConfirming(false) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {mode === "reset" ? t("traffic.calibration_confirm_title") : t("traffic.calibration_confirm_title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("traffic.calibration_confirm_description")} {mode === "reset" ? t("traffic.calibration_reset_warning") : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("app.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={props.pending}
              onClick={() => props.onSubmit({ request_id: requestId, ...draft.payload }, mode)}
            >
              {t("traffic.calibration_confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("traffic.calibrate")}</DialogTitle>
          <DialogDescription>{t("traffic.calibration_description")}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel>{t("traffic.calibration_mode")}</FieldLabel>
            <ToggleGroup variant="outline" spacing={0} value={[mode]} onValueChange={(value) => value[0] && setMode(value[0] as TrafficCalibrationMode)}>
              {(["set", "reset"] as TrafficCalibrationMode[]).map((option) => (
                <ToggleGroupItem key={option} value={option} disabled={props.pending} aria-label={t(`traffic.calibration_${option}`)}>
                  {t(`traffic.calibration_${option}`)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
          <Field>
            <FieldLabel htmlFor="traffic-calibration-upload">{t("traffic.calibration_upload")}</FieldLabel>
            <Input
              id="traffic-calibration-upload"
              value={mode === "reset" ? "0" : upload}
              disabled={mode === "reset" || props.pending}
              placeholder="1.5 GiB"
              onChange={(event) => setUpload(event.target.value)}
            />
            <FieldDescription>{t("traffic.calibration_upload_hint")}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="traffic-calibration-download">{t("traffic.calibration_download")}</FieldLabel>
            <Input
              id="traffic-calibration-download"
              value={mode === "reset" ? "0" : download}
              disabled={mode === "reset" || props.pending}
              placeholder="1.5 GiB"
              onChange={(event) => setDownload(event.target.value)}
            />
            <FieldDescription>{t("traffic.calibration_download_hint")}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="traffic-calibration-effective">{t("traffic.calibration_effective")}</FieldLabel>
            <Input
              id="traffic-calibration-effective"
              type="datetime-local"
              value={effective}
              disabled={props.pending}
              onChange={(event) => setEffective(event.target.value)}
            />
            <FieldDescription>{t("traffic.calibration_effective_hint")}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="traffic-calibration-reason">{t("traffic.calibration_reason")}</FieldLabel>
            <Textarea
              id="traffic-calibration-reason"
              value={reason}
              maxLength={maximumReasonLength}
              disabled={props.pending}
              onChange={(event) => setReason(event.target.value)}
            />
            <FieldDescription>{t("traffic.calibration_reason_hint")}</FieldDescription>
            {draft.code ? <FieldError>{validationMessage}</FieldError> : null}
          </Field>
          <Alert>
            <AlertDescription>
              <span className="block">{t("traffic.calibration_current")}: {formatBytes(node.traffic?.used ?? 0)}</span>
              <span className="block">{t("traffic.calibration_raw")}: {t("node.traffic")} {formatTrafficTotals(node)}</span>
            </AlertDescription>
          </Alert>
          {mode === "reset" ? <Alert variant="destructive"><AlertDescription>{t("traffic.calibration_reset_warning")}</AlertDescription></Alert> : null}
          {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        </FieldGroup>
        <DialogFooter className="sm:justify-between">
          <Button variant="ghost" onClick={props.onOpenHistory} disabled={props.pending}>
            <GaugeIcon />{t("traffic.calibration_history")}
          </Button>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => close(false)} disabled={props.pending}>{t("app.cancel")}</Button>
            <Button onClick={submit} disabled={props.pending || Boolean(draft.code)}>
              {t("traffic.calibration_submit")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface DraftInput {
  mode: TrafficCalibrationMode
  upload: string
  download: string
  reason: string
  effective: string
}

interface Draft {
  payload: Omit<TrafficCalibrationPayload, "request_id">
  code: "" | "reason" | "bytes" | "effective"
}

export function buildDraft(input: DraftInput): Draft {
  const reason = input.reason.trim()
  const payload: Omit<TrafficCalibrationPayload, "request_id"> = {
    target_sent: "0", target_received: "0", reason,
  }
  if (reason.length === 0 || reason.length > maximumReasonLength) {
    return { payload, code: "reason" }
  }
  if (input.mode === "reset") {
    return { payload, code: "" }
  }
  const sent = parseCalibrationBytes(input.upload)
  const received = parseCalibrationBytes(input.download)
  if (sent === undefined || received === undefined) {
    return { payload, code: "bytes" }
  }
  payload.target_sent = sent
  payload.target_received = received
  if (input.effective !== "") {
    const instant = new Date(input.effective)
    if (Number.isNaN(instant.getTime())) return { payload, code: "effective" }
    payload.effective_at = instant.toISOString()
  }
  return { payload, code: "" }
}

function messageFor(code: Draft["code"], t: (key: string) => string): string {
  if (code === "reason") return t("traffic.calibration_reason_required")
  if (code === "bytes") return t("traffic.calibration_invalid_bytes")
  if (code === "effective") return t("traffic.calibration_effective_hint")
  return ""
}

function formatTrafficTotals(node: ManagedNode): string {
  const traffic = node.traffic
  if (!traffic) return "--"
  return `${formatCalibrationBytes(String(round(traffic.sent)))} ↑ / ${formatCalibrationBytes(String(round(traffic.received)))} ↓`
}

function round(value: number): number {
  return Math.max(0, Math.round(value))
}
