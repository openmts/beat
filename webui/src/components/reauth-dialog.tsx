import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"

// 敏感管理操作的统一重认证入口：确认身份后重放被 428 拦下的动作，避免界面停在
// 一句后端英文错误上（见 use-reauth-retry）。文案放在基础目录，任何后台页面都能复用。
export function ReauthDialog({ codeRequired, pending, error, onClose, onConfirm }: {
  codeRequired: boolean
  pending: boolean
  error: string
  onClose: () => void
  onConfirm: (password: string, code: string) => void
}) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pending) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("reauth.title")}</DialogTitle>
          <DialogDescription>{t("reauth.description")}</DialogDescription>
        </DialogHeader>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="reauth-password">{t("reauth.password")}</FieldLabel>
            <Input id="reauth-password" type="password" autoComplete="current-password"
              value={password} onChange={(event) => setPassword(event.target.value)} />
          </Field>
          {codeRequired && (
            <Field>
              <FieldLabel htmlFor="reauth-code">{t("reauth.code")}</FieldLabel>
              <Input id="reauth-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
                value={code} onChange={(event) => setCode(event.target.value)} />
            </Field>
          )}
        </FieldGroup>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={pending} onClick={onClose}>{t("app.cancel")}</Button>
          <Button type="button" disabled={pending || password.length === 0}
            onClick={() => onConfirm(password, code)}>{t("reauth.confirm")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
