import { useState, type FormEvent } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { rejectEnrollmentRequest } from "@/lib/enrollment-discovery-api"
import type { EnrollmentRequest } from "@/types/enrollment"

const maximumReasonLength = 64

interface EnrollmentRejectDialogProps {
  request: EnrollmentRequest
  onClose: () => void
  onRejected: (request: EnrollmentRequest) => void
}

/** 拒绝只需要一个有界原因码，机器读得到但看不到任何内部信息。 */
export function EnrollmentRejectDialog({ request, onClose, onRejected }: EnrollmentRejectDialogProps) {
  const { t } = useLocale()
  const [reason, setReason] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (busy) return
    setError("")
    setBusy(true)
    try {
      onRejected(await rejectEnrollmentRequest(request.id, reason.trim()))
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("discovery.grant_error"))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t("discovery.reject_title")}</DialogTitle>
            <DialogDescription>{request.hostname || request.source_ip}</DialogDescription>
          </DialogHeader>
          <fieldset disabled={busy} className="py-4">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="enrollment-reject-reason">{t("discovery.reject_reason")}</FieldLabel>
                <Input id="enrollment-reject-reason" value={reason} maxLength={maximumReasonLength}
                  onChange={(event) => setReason(event.target.value)} />
                <p className="text-xs text-muted-foreground">{t("discovery.reject_reason_hint")}</p>
              </Field>
            </FieldGroup>
          </fieldset>
          {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>{t("app.cancel")}</Button>
            <Button type="submit" variant="destructive" disabled={busy}>{t("discovery.reject")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
