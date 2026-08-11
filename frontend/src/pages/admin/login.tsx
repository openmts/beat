import { useState, type FormEvent } from "react"
import { KeyRoundIcon, ShieldCheckIcon } from "lucide-react"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

export default function AdminLogin() {
  const { login, bootstrap, setupRequired } = useAuth()
  const { t } = useLocale()
  const { settings } = useSiteSettings()
  const [username, setUsername] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [bootstrapToken, setBootstrapToken] = useState("")
  const [totpCode, setTOTPCode] = useState("")
  const [needsTOTP, setNeedsTOTP] = useState(false)
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    try {
      if (setupRequired) {
        await bootstrap({ bootstrapToken, username, displayName, password })
      } else {
        await login({ username, password, totpCode })
      }
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : t("auth.invalid")
      if (message.includes("TOTP")) setNeedsTOTP(true)
      setError(message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="grid min-h-svh place-items-center bg-muted/30 px-4 py-8">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          {settings.logo_url ? (
            <img
              src={settings.logo_url}
              alt=""
              className="size-12 rounded-lg object-contain"
              referrerPolicy="no-referrer"
            />
          ) : null}
          <div>
            <h1 className="text-xl font-bold tracking-tight">{settings.site_title || t("app.title")}</h1>
            <p className="mt-1 text-sm text-muted-foreground">{t("security.login_description")}</p>
          </div>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{setupRequired ? t("security.setup_title") : t("auth.title")}</CardTitle>
            <CardDescription>
              {setupRequired ? t("security.setup_description") : t("auth.description")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit}>
              <FieldGroup>
                {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
                {setupRequired && (
                  <Field>
                    <FieldLabel htmlFor="bootstrap-token">{t("security.bootstrap_token")}</FieldLabel>
                    <Input id="bootstrap-token" type="password" value={bootstrapToken}
                      onChange={(event) => setBootstrapToken(event.target.value)} required />
                  </Field>
                )}
                <Field>
                  <FieldLabel htmlFor="admin-username">{t("security.username")}</FieldLabel>
                  <Input id="admin-username" autoComplete="username" value={username}
                    onChange={(event) => setUsername(event.target.value)} required />
                </Field>
                {setupRequired && (
                  <Field>
                    <FieldLabel htmlFor="display-name">{t("security.display_name")}</FieldLabel>
                    <Input id="display-name" value={displayName}
                      onChange={(event) => setDisplayName(event.target.value)} required />
                  </Field>
                )}
                <Field>
                  <FieldLabel htmlFor="admin-password">{t("security.password")}</FieldLabel>
                  <Input id="admin-password" type="password" autoComplete="current-password"
                    value={password} onChange={(event) => setPassword(event.target.value)} required />
                </Field>
                {!setupRequired && needsTOTP && (
                  <Field>
                    <FieldLabel htmlFor="totp-code">{t("security.totp_code")}</FieldLabel>
                    <Input id="totp-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
                      value={totpCode} onChange={(event) => setTOTPCode(event.target.value)} required />
                  </Field>
                )}
                <Button type="submit" className="w-full" disabled={submitting || !username.trim() || !password}>
                  {setupRequired ? <ShieldCheckIcon data-icon="inline-start" /> : <KeyRoundIcon data-icon="inline-start" />}
                  {setupRequired ? t("security.create_owner") : t("auth.connect")}
                </Button>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      </div>
    </main>
  )
}
