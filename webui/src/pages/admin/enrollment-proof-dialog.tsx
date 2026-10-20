import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { reauthenticateAdmin } from "@/lib/security-api"

// 授权的读取与写入都要求近期重认证：就地确认身份，成功后由调用方重新读取，避免只留一个
// 永远点不成功的「重试」按钮。
export function EnrollmentProofDialog({ codeRequired, onClose, onConfirmed }: {
  codeRequired: boolean
  onClose: () => void
  onConfirmed: () => void
}) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    setError("")
    try {
      await reauthenticateAdmin(password, code)
      onConfirmed()
      onClose()
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("discovery.proof_failed"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("discovery.proof_title")}</DialogTitle>
          <DialogDescription>{t("discovery.proof_required")}</DialogDescription>
        </DialogHeader>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="enrollment-proof-password">{t("discovery.proof_password")}</FieldLabel>
            <Input id="enrollment-proof-password" type="password" autoComplete="current-password"
              value={password} onChange={(event) => setPassword(event.target.value)} />
          </Field>
          {codeRequired && (
            <Field>
              <FieldLabel htmlFor="enrollment-proof-code">{t("discovery.proof_code")}</FieldLabel>
              <Input id="enrollment-proof-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
                value={code} onChange={(event) => setCode(event.target.value)} />
            </Field>
          )}
        </FieldGroup>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t("app.cancel")}</Button>
          <Button type="button" disabled={busy || password.length === 0} onClick={() => void submit()}>
            {t("discovery.proof_confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
