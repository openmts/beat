import { useState, type FormEvent } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import type { useServiceTokenActions } from "@/hooks/use-service-token-actions"
import type { ServiceTokenCredential } from "@/types/service-tokens"

type Flow = ReturnType<typeof useServiceTokenActions>

export function ServiceTokenActionDialog({ flow }: { flow: Flow }) {
  return <Dialog open={flow.action !== null} onOpenChange={open => { if (!open) flow.close() }}>
    <DialogContent><DialogHeader><DialogTitle>{flow.action?.title}</DialogTitle>
      <DialogDescription>{flow.action?.description}</DialogDescription></DialogHeader>
      {flow.action && <ActionForm flow={flow} />}</DialogContent>
  </Dialog>
}

function ActionForm({ flow }: { flow: Flow }) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    await flow.confirm(password, code)
    setPassword("")
    setCode("")
  }
  return <form onSubmit={submit}><FieldGroup>
    {flow.error && <Alert variant="destructive" role="alert"><AlertDescription>{t("security.tokens.error")}</AlertDescription></Alert>}
    {flow.needsProof && <>
      <Field><FieldLabel htmlFor="service-token-password">{t("security.current_password")}</FieldLabel>
        <Input id="service-token-password" type="password" autoComplete="current-password" required
          value={password} disabled={flow.busy} onChange={event => setPassword(event.target.value)} /></Field>
      {flow.totp && <Field><FieldLabel htmlFor="service-token-code">{t("security.totp_code")}</FieldLabel>
        <Input id="service-token-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} required
          value={code} disabled={flow.busy} onChange={event => setCode(event.target.value)} /></Field>}
    </>}
    <div className="flex flex-wrap gap-2">
      <Button type="submit" disabled={flow.busy || (flow.needsProof && (!password || (flow.totp && !/^\d{6}$/.test(code))))}>
        {flow.needsProof ? t("security.reauthenticate") : t("security.tokens.confirm")}</Button>
      <Button type="button" variant="outline" disabled={flow.busy} onClick={flow.close}>{t("app.cancel")}</Button>
    </div>
  </FieldGroup></form>
}

export function ServiceTokenSecretDialog({ credential, dismiss }: { credential: ServiceTokenCredential | null; dismiss: () => void }) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (!credential) return
    try {
      await navigator.clipboard.writeText(credential.token)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }
  const close = () => { setCopied(false); dismiss() }
  return <Dialog open={credential !== null} onOpenChange={open => { if (!open) close() }}>
    <DialogContent><DialogHeader><DialogTitle>{t("security.tokens.secret_title")}</DialogTitle>
      <DialogDescription>{t("security.tokens.secret_help")}</DialogDescription></DialogHeader>
      {credential && <div className="flex flex-col gap-3">
        <code className="block overflow-x-auto rounded-md border bg-muted/40 p-3 text-xs">{credential.token}</code>
        <p className="text-sm text-muted-foreground">{credential.principal.name} · {credential.principal.prefix}</p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={() => void copy()}>
            {t(copied ? "security.tokens.copied" : "security.tokens.copy")}</Button>
          <Button type="button" onClick={close}>{t("security.tokens.stored")}</Button>
        </div>
      </div>}</DialogContent>
  </Dialog>
}
