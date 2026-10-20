import { useEffect, useState } from "react"
import { CheckCircle2Icon, KeyRoundIcon, Loader2Icon, TerminalIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { createAgentEnrollmentToken, type AgentEnrollmentCommand } from "@/lib/agent-enrollment-api"
import { AgentSetupCode } from "@/components/agent-setup-code"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"

export function AgentEnrollment({ serverURL }: { serverURL: string | null }) {
  const { t } = useLocale()
  const [allowHTTP, setAllowHTTP] = useState(false)
  const { command, loading, error, generate } = useEnrollmentCommand(serverURL, allowHTTP)
  const insecure = serverURL?.startsWith("http://") === true
  const disabled = !serverURL || (insecure && !allowHTTP) || loading

  return (
    <div className="grid min-w-0 gap-5">
      <Card>
        <CardHeader>
          <CardTitle><h3>{t("enrollment.title")}</h3></CardTitle>
          <CardDescription>{t("enrollment.description")}</CardDescription>
        </CardHeader>
        <CardContent className="min-w-0 space-y-4">
          <p className="text-sm leading-relaxed text-muted-foreground">{t("enrollment.prerequisite")}</p>
          {insecure && <Label className="flex items-start gap-3 rounded-lg border p-3 leading-relaxed">
            <Checkbox checked={allowHTTP} onCheckedChange={(checked) => setAllowHTTP(checked === true)} className="mt-0.5" />
            <span>{t("enrollment.allow_http")}</span>
          </Label>}
          {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
          <Button disabled={disabled} onClick={generate}>
            {loading ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />}
            {t(loading ? "enrollment.generating" : command ? "enrollment.regenerate" : "enrollment.generate")}
          </Button>
          {command && <EnrollmentCommand key={command.id} value={command} />}
          <p className="text-xs leading-relaxed text-muted-foreground">{t("enrollment.lifetime")}</p>
        </CardContent>
      </Card>
      <EnrollmentHelp />
    </div>
  )
}

function useEnrollmentCommand(serverURL: string | null, allowHTTP: boolean) {
  const { t } = useLocale()
  const [command, setCommand] = useState<AgentEnrollmentCommand | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const generate = async () => {
    if (!serverURL || loading) return
    setCommand(null)
    setError(null)
    setLoading(true)
    try {
      setCommand(await createAgentEnrollmentToken({ server_url: serverURL, allow_http: allowHTTP }))
    } catch (error) {
      setError(error instanceof Error ? error.message : t("enrollment.generate_error"))
    } finally {
      setLoading(false)
    }
  }
  return { command, loading, error, generate }
}

function EnrollmentCommand({ value }: { value: AgentEnrollmentCommand }) {
  const { locale, t } = useLocale()
  const [expired, setExpired] = useState(() => Date.parse(value.expires_at) <= Date.now())
  useEffect(() => {
    const timer = setTimeout(() => setExpired(true), Math.max(0, Date.parse(value.expires_at) - Date.now()))
    return () => clearTimeout(timer)
  }, [value.expires_at])

  return (
    <div className="space-y-3">
      {expired ? <Alert variant="destructive"><AlertDescription>{t("enrollment.expired")}</AlertDescription></Alert> : <>
        <AgentSetupCode title={t("enrollment.command")} code={value.command} />
        <p className="text-xs text-muted-foreground">
          {t("enrollment.expires")} <time dateTime={value.expires_at}>{formatDateTime(locale, value.expires_at)}</time>
        </p>
        <p className="text-sm leading-relaxed text-muted-foreground">{t("enrollment.run_hint")}</p>
      </>}
    </div>
  )
}

function EnrollmentHelp() {
  const { t } = useLocale()
  return (
    <div className="grid min-w-0 gap-5 xl:grid-cols-2">
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2"><CheckCircle2Icon className="size-4" /><h3>{t("enrollment.automatic_title")}</h3></CardTitle></CardHeader>
        <CardContent>
          <ol className="list-decimal space-y-3 pl-5 text-sm leading-relaxed text-muted-foreground">
            {["identify", "config", "systemd"].map((key) => <li key={key}>{t(`enrollment.automatic_${key}`)}</li>)}
          </ol>
          <p className="mt-4 text-sm leading-relaxed text-muted-foreground">{t("enrollment.verify")}</p>
        </CardContent>
      </Card>
      <Card className="min-w-0">
        <CardHeader><CardTitle className="flex items-center gap-2"><TerminalIcon className="size-4" /><h3>{t("enrollment.recovery_title")}</h3></CardTitle></CardHeader>
        <CardContent className="min-w-0 space-y-4 text-sm leading-relaxed [&>p]:text-muted-foreground">
          <p>{t("enrollment.recovery")}</p>
          <AgentSetupCode title={t("enrollment.service_command")} code="sudo ./beat-agent service install --config /etc/beat-agent/agent.json" />
          <p>{t("enrollment.customize")}</p>
        </CardContent>
      </Card>
    </div>
  )
}
