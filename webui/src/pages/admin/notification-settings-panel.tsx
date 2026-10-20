import { useLocale } from "@/context/locale"
import { useAuth } from "@/context/auth"
import { useNotificationSettings } from "@/hooks/use-notification-settings"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { NotificationConfirmDialog } from "@/pages/admin/notification-confirm-dialog"
import { NotificationEgressFields } from "@/pages/admin/notification-egress-fields"

export default function NotificationSettingsPanel() {
  const { principal } = useAuth()
  if (principal?.user.role !== "owner") return null
  return <NotificationSettingsContent />
}

function NotificationSettingsContent() {
  const { t } = useLocale()
  const flow = useNotificationSettings()
  const form = flow.form
  const disabled = !flow.owner.owner || flow.owner.busy
  return <div className="grid gap-4">
    {flow.error && <Alert variant="destructive" role="alert"><AlertDescription>{t(flow.error)}</AlertDescription></Alert>}
    {flow.saved && <Alert role="status"><AlertDescription>{t("notification.saved")}</AlertDescription></Alert>}
    {form ? <>
      <Card><CardHeader><CardTitle>{t("notification.settings")}</CardTitle><CardDescription>{t("notification.settings_help")}</CardDescription></CardHeader><CardContent>
        <fieldset disabled={disabled} className="grid gap-4 sm:grid-cols-3">
          <Field><FieldLabel htmlFor="notification-success-days">{t("notification.success_days")}</FieldLabel><Input id="notification-success-days" type="number" min={7} max={365} value={form.successDays} onChange={event => flow.change({ successDays: event.target.value })} /></Field>
          <Field><FieldLabel htmlFor="notification-failed-days">{t("notification.failed_days")}</FieldLabel><Input id="notification-failed-days" type="number" min={30} max={730} value={form.failedDays} onChange={event => flow.change({ failedDays: event.target.value })} /></Field>
          <Field><FieldLabel htmlFor="notification-workers">{t("notification.workers")}</FieldLabel><Input id="notification-workers" type="number" min={1} max={32} value={form.workers} onChange={event => flow.change({ workers: event.target.value })} /></Field>
        </fieldset>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>{t("notification.egress")}</CardTitle><CardDescription>{t("notification.egress_description")}</CardDescription></CardHeader><CardContent>
        <NotificationEgressFields entries={form.exceptions} onChange={exceptions => flow.change({ exceptions })} disabled={disabled} />
      </CardContent></Card>
      <div className="flex flex-wrap gap-2">
        {flow.owner.owner && <Button disabled={disabled} onClick={flow.save}>{t("app.save")}</Button>}
      </div>
    </> : <p role="status" className="text-sm text-muted-foreground">{t(flow.loading ? "notification.loading" : "notification.unavailable")}</p>}
    <Button className="justify-self-start" variant="outline" disabled={flow.owner.busy || flow.loading} onClick={() => void flow.refresh()}>{t("notification.reload")}</Button>
    <NotificationConfirmDialog flow={flow.owner} />
  </div>
}
