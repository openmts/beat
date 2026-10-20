import { useState, type FormEvent } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import type { useNotificationOwnerAction } from "@/hooks/use-notification-owner-action"

export function NotificationConfirmDialog({ flow }: { flow: ReturnType<typeof useNotificationOwnerAction> }) {
  if (!flow.action) return null
  return <ConfirmContent key={flow.action.title} flow={flow} />
}

function ConfirmContent({ flow }: { flow: ReturnType<typeof useNotificationOwnerAction> }) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [duplicate, setDuplicate] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    await flow.confirm(password, code)
    setPassword(""); setCode("")
  }
  const disabled = flow.busy || Boolean(flow.action?.duplicate && !duplicate)
    || (flow.needsProof && (!password || (flow.totp && !/^\d{6}$/.test(code))))
  return <Dialog open onOpenChange={open => { if (!open) flow.close() }}><DialogContent>
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader><DialogTitle>{flow.action?.title}</DialogTitle><DialogDescription>{flow.action?.description}</DialogDescription></DialogHeader>
      {flow.needsProof && <>
        <p className="text-sm text-muted-foreground">{t("notification.identity_help")}</p>
        <Field><FieldLabel htmlFor="notification-owner-password">{t("security.current_password")}</FieldLabel>
          <Input id="notification-owner-password" type="password" autoComplete="current-password" value={password} disabled={flow.busy} onChange={event => setPassword(event.target.value)} /></Field>
        {flow.totp && <Field><FieldLabel htmlFor="notification-owner-totp">{t("security.totp_code")}</FieldLabel>
          <Input id="notification-owner-totp" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} disabled={flow.busy} onChange={event => setCode(event.target.value)} /></Field>}
      </>}
      {flow.action?.duplicate && <label className="flex items-start gap-2 text-sm"><Checkbox checked={duplicate} onCheckedChange={checked => setDuplicate(Boolean(checked))} />{t("notification.duplicate_confirm")}</label>}
      {flow.error && <Alert variant="destructive" role="alert"><AlertDescription>{t(flow.error)}</AlertDescription></Alert>}
      <DialogFooter><Button type="button" variant="outline" disabled={flow.busy} onClick={flow.close}>{t("app.cancel")}</Button>
        <Button type="submit" disabled={disabled}>{flow.busy ? t("notification.working") : t("notification.confirm")}</Button></DialogFooter>
    </form>
  </DialogContent></Dialog>
}
