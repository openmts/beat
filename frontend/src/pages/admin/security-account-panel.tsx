import { useState, type FormEvent } from "react"
import { KeyRoundIcon, ShieldCheckIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { changeAdminPassword, reauthenticateAdmin } from "@/lib/security-api"

export function SecurityAccountPanel() {
  const { principal } = useAuth()
  const { t } = useLocale()
  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [totpCode, setTOTPCode] = useState("")
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")

  const reauthenticate = async (event: FormEvent) => {
    event.preventDefault()
    await runAction(async () => {
      await reauthenticateAdmin(currentPassword, totpCode)
      setMessage(t("security.reauthenticated"))
    })
  }

  const changePassword = async (event: FormEvent) => {
    event.preventDefault()
    await runAction(async () => {
      await changeAdminPassword({
        current_password: currentPassword,
        new_password: newPassword,
        totp_code: totpCode,
      })
      setMessage(t("security.password_changed"))
      setCurrentPassword("")
      setNewPassword("")
    })
  }

  const runAction = async (action: () => Promise<void>) => {
    setError("")
    setMessage("")
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>{principal?.user.display_name}</CardTitle>
          <CardDescription>@{principal?.user.username} · {t(`security.${principal?.user.role}`)}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={reauthenticate}>
            <FieldGroup>
              {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
              {message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
              <CredentialFields idPrefix="reauth" currentPassword={currentPassword} totpCode={totpCode}
                onPassword={setCurrentPassword} onTOTP={setTOTPCode} />
              <Button type="submit"><ShieldCheckIcon data-icon="inline-start" />{t("security.reauthenticate")}</Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t("security.change_password")}</CardTitle>
          <CardDescription>{t("security.password_changed")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={changePassword}>
            <FieldGroup>
              <CredentialFields idPrefix="password-change" currentPassword={currentPassword} totpCode={totpCode}
                onPassword={setCurrentPassword} onTOTP={setTOTPCode} />
              <Field>
                <FieldLabel htmlFor="new-password">{t("security.new_password")}</FieldLabel>
                <Input id="new-password" type="password" minLength={12} value={newPassword}
                  onChange={(event) => setNewPassword(event.target.value)} required />
              </Field>
              <Button type="submit"><KeyRoundIcon data-icon="inline-start" />{t("security.change_password")}</Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

function CredentialFields({ idPrefix, currentPassword, totpCode, onPassword, onTOTP }: {
  idPrefix: string
  currentPassword: string
  totpCode: string
  onPassword: (value: string) => void
  onTOTP: (value: string) => void
}) {
  const { t } = useLocale()
  return (
    <>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-current-password`}>{t("security.current_password")}</FieldLabel>
        <Input id={`${idPrefix}-current-password`} type="password" value={currentPassword}
          onChange={(event) => onPassword(event.target.value)} required />
      </Field>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-totp`}>{t("security.totp_code")}</FieldLabel>
        <Input id={`${idPrefix}-totp`} inputMode="numeric" maxLength={6} value={totpCode}
          onChange={(event) => onTOTP(event.target.value)} />
      </Field>
    </>
  )
}
