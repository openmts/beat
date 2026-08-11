import { useState } from "react"
import { ShieldCheckIcon, ShieldOffIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { beginTOTP, disableTOTP, enableTOTP } from "@/lib/security-api"
import type { TOTPSetup } from "@/types/security"

export function SecurityTOTPPanel() {
  const { principal, refresh } = useAuth()
  const { t } = useLocale()
  const [setup, setSetup] = useState<TOTPSetup | null>(null)
  const [code, setCode] = useState("")
  const [error, setError] = useState("")
  const enabled = principal?.user.totp_enabled_at !== null

  const run = async (action: () => Promise<void>) => {
    setError("")
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("security.two_factor")}</CardTitle>
        <CardDescription>{enabled ? t("security.totp_enabled") : t("security.totp_disabled")}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup>
          {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
          {setup && (
            <Alert>
              <AlertDescription className="break-all">
                {t("security.totp_secret")}: <span className="font-mono">{setup.secret}</span>
              </AlertDescription>
            </Alert>
          )}
          {!enabled && !setup && (
            <Button onClick={() => void run(async () => setSetup(await beginTOTP()))}>
              <ShieldCheckIcon data-icon="inline-start" />{t("security.begin_totp")}
            </Button>
          )}
          {!enabled && setup && (
            <>
              <Field>
                <FieldLabel htmlFor="enable-totp">{t("security.totp_code")}</FieldLabel>
                <Input id="enable-totp" inputMode="numeric" maxLength={6} value={code}
                  onChange={(event) => setCode(event.target.value)} />
              </Field>
              <Button onClick={() => void run(async () => {
                await enableTOTP(code)
                setSetup(null)
                await refresh()
              })}>{t("security.enable_totp")}</Button>
            </>
          )}
          {enabled && (
            <Button variant="destructive" onClick={() => void run(async () => {
              await disableTOTP()
              await refresh()
            })}>
              <ShieldOffIcon data-icon="inline-start" />{t("security.disable_totp")}
            </Button>
          )}
        </FieldGroup>
      </CardContent>
    </Card>
  )
}
