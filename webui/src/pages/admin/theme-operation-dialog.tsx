import { useState, type FormEvent } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import type { useThemeOperation } from "@/hooks/use-theme-operation"

export function ThemeOperationDialog({ flow }: { flow: ReturnType<typeof useThemeOperation> }) {
  const { t } = useLocale()
  return <Dialog open={flow.operation !== null} onOpenChange={(open) => { if (!open) flow.cancel() }}>
    <DialogContent><DialogHeader><DialogTitle>{flow.operation?.label}</DialogTitle>
      <DialogDescription>{flow.proof ? t("theme.proof_hint") : flow.operation?.description}</DialogDescription>
    </DialogHeader>{flow.operation && <OperationForm key={`${flow.operation.label}:${flow.proof}`} flow={flow} />}</DialogContent>
  </Dialog>
}

function OperationForm({ flow }: { flow: ReturnType<typeof useThemeOperation> }) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const submit = (event: FormEvent) => { event.preventDefault(); flow.submit(password, code) }
  return <form onSubmit={submit}><FieldGroup>
    {flow.error && <Alert variant="destructive"><AlertDescription>{flow.error}</AlertDescription></Alert>}
    {flow.proof && <>
      <Field><FieldLabel htmlFor="theme-proof-password">{t("security.current_password")}</FieldLabel>
        <Input id="theme-proof-password" type="password" autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} /></Field>
      <Field><FieldLabel htmlFor="theme-proof-code">{t("security.totp_code")}</FieldLabel>
        <Input id="theme-proof-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(event) => setCode(event.target.value)} /></Field>
    </>}
    <div className="flex flex-wrap gap-2"><Button type="submit" disabled={flow.loading}>{t(flow.proof ? "theme.verify_continue" : "theme.confirm")}</Button>
      <Button variant="outline" disabled={flow.loading} onClick={flow.cancel}>{t("app.cancel")}</Button></div>
  </FieldGroup></form>
}
