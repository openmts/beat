import { useCallback, useEffect, useState, type FormEvent } from "react"
import { PlayIcon, RotateCcwIcon, SaveIcon } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import {
  getAgentRetention,
  resetAgentRetention,
  runAgentRetention,
  updateAgentRetention,
} from "@/lib/agent-retention-api"
import type { AgentRetentionSettings } from "@/types/agent-retention"

const fields = "agentRetention"

/** 发布对象保留期：后台可配置，未配置时沿用服务端启动缺省。 */
function AgentRetentionPanel() {
  const { t } = useLocale()
  const [settings, setSettings] = useState<AgentRetentionSettings | null>(null)
  const [rolloutDays, setRolloutDays] = useState(0)
  const [releaseDays, setReleaseDays] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)

  const apply = useCallback((next: AgentRetentionSettings) => {
    setSettings(next)
    setRolloutDays(next.rollout_days)
    setReleaseDays(next.release_days)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void getAgentRetention(controller.signal)
      .then((next) => { if (!controller.signal.aborted) apply(next) })
      .catch((reason: unknown) => { if (!controller.signal.aborted) setError(errorMessage(reason, t("app.error"))) })
    return () => { controller.abort() }
  }, [apply, t])

  const perform = useCallback(async (action: () => Promise<AgentRetentionSettings>, done: string) => {
    setBusy(true)
    setMessage(null)
    try {
      apply(await action())
      setMessage(t(done))
      setError(null)
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    } finally {
      setBusy(false)
    }
  }, [apply, t])

  const save = (event: FormEvent) => {
    event.preventDefault()
    void perform(() => updateAgentRetention({ rollout_days: rolloutDays, release_days: releaseDays }), `${fields}.saved`)
  }
  const run = () => {
    if (!window.confirm(t(`${fields}.run_confirm`))) return
    void perform(runAgentRetention, `${fields}.run_started`)
  }
  const reset = () => {
    if (!window.confirm(t(`${fields}.reset_confirm`))) return
    void perform(resetAgentRetention, `${fields}.reset_done`)
  }

  return (
    <section className="space-y-4 rounded-lg border p-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="font-semibold">{t(`${fields}.title`)}</h2>
          <p className="text-sm text-muted-foreground">{t(`${fields}.description`)}</p>
        </div>
        <Badge variant={settings?.configured ? "default" : "outline"}>
          {t(settings?.configured ? `${fields}.configured` : `${fields}.environment_default`)}
        </Badge>
      </header>
      {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      {message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
      <p className="text-sm text-muted-foreground">{t(`${fields}.environment_hint`, {
        rollout: settings?.environment.rollout_days ?? 0,
        release: settings?.environment.release_days ?? 0,
        enabled: String(settings?.environment.enabled ?? false),
      })}</p>
      <RunSummary settings={settings} />
      <form className="space-y-4" onSubmit={save}>
        <FieldGroup>
          <DayField id="retention-rollout-days" label={t(`${fields}.rollout_days`)} hint={t(`${fields}.rollout_hint`)}
            value={rolloutDays} disabled={busy} onChange={setRolloutDays} />
          <DayField id="retention-release-days" label={t(`${fields}.release_days`)} hint={t(`${fields}.release_hint`)}
            value={releaseDays} disabled={busy} onChange={setReleaseDays} />
        </FieldGroup>
        <div className="flex flex-wrap justify-end gap-2 border-t pt-4">
          <Button type="button" variant="outline" disabled={busy || !settings?.configured} onClick={reset}>
            <RotateCcwIcon />{t(`${fields}.reset`)}
          </Button>
          <Button type="button" variant="outline" disabled={busy} onClick={run}>
            <PlayIcon />{t(`${fields}.run`)}
          </Button>
          <Button type="submit" disabled={busy}>{busy ? null : <SaveIcon />}{t("app.save")}</Button>
        </div>
      </form>
    </section>
  )
}

function DayField(props: {
  id: string
  label: string
  hint: string
  value: number
  disabled: boolean
  onChange: (value: number) => void
}) {
  const { t } = useLocale()
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <div className="flex max-w-xs items-center gap-2">
        <Input
          id={props.id}
          type="number"
          min={0}
          max={3650}
          value={props.value}
          disabled={props.disabled}
          onChange={(event) => props.onChange(Number(event.target.value))}
        />
        <span className="text-sm text-muted-foreground">{t(`${fields}.days`)}</span>
      </div>
      <FieldDescription>{props.hint}</FieldDescription>
      <p className="text-xs text-muted-foreground">{t(`${fields}.zero_hint`)}</p>
    </Field>
  )
}

function RunSummary({ settings }: { settings: AgentRetentionSettings | null }) {
  const { locale, t } = useLocale()
  if (!settings?.status.last_run_at) {
    return <p className="text-sm text-muted-foreground">{t(`${fields}.never`)}</p>
  }
  const status = settings.status
  const state = t(`${fields}.${status.last_status}`)
  const trigger = t(status.last_trigger === "manual" ? `${fields}.trigger_manual` : `${fields}.trigger_schedule`)
  return (
    <div className="grid gap-2 text-sm text-muted-foreground sm:grid-cols-2">
      <p>
        <span className="text-foreground">{t(`${fields}.last_run`)}:</span>{" "}
        {state} · {trigger} · {formatDateTime(locale, status.last_run_at)}
      </p>
      <p>
        <span className="text-foreground">{t(`${fields}.deleted`)}:</span>{" "}
        {t(`${fields}.deleted_value`, {
          rollouts: status.last_rollouts,
          releases: status.last_releases,
          artifacts: status.last_artifacts,
        })}
      </p>
      {status.last_error && <p className="text-destructive sm:col-span-2">{status.last_error}</p>}
    </div>
  )
}

function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback
}

export default AgentRetentionPanel
export { AgentRetentionPanel }
