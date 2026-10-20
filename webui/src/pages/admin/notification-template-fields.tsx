import type { Dispatch, SetStateAction } from "react"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import type { AlertChannelFormState } from "@/pages/admin/alert-channel-config"

const commonTemplate = "[{{event.severity}}] {{rule.name}}\nNode: {{node.name}}\nMetric: {{metric.name}} = {{metric.formatted_value}}\nStatus: {{event.status}}\n{{event.message}}"
const templateTokens = ["event.message", "event.kind", "event.status", "event.severity", "event.occurred_at", "node.name", "rule.name", "metric.formatted_value", "delivery.id", "delivery.attempt"]


export function NotificationTemplateFields({ form, setForm }: { form: AlertChannelFormState; setForm: Dispatch<SetStateAction<AlertChannelFormState>> }) {
  const { t } = useLocale()
  return (
        <details className="rounded-md border px-4 py-3"><summary className="cursor-pointer text-sm font-medium">{t("notification.templates")}</summary>
          <div className="mt-4 grid gap-4">
            <Field><FieldLabel htmlFor="channel-subject">{t("notification.subject_template")}</FieldLabel><Input id="channel-subject" value={form.templateSubject} onChange={event => setForm(current => ({ ...current, templateSubject: event.target.value }))} /></Field>
            <Field><FieldLabel htmlFor="channel-template">{t("notification.text_template")}</FieldLabel><Textarea id="channel-template" rows={5} value={form.template} onChange={event => setForm(current => ({ ...current, template: event.target.value }))} /><FieldDescription>{t("notification.template_help")}</FieldDescription></Field>
            <Button type="button" variant="outline" onClick={() => setForm(current => ({ ...current, template: commonTemplate }))}>{t("alert.template_use_common")}</Button>
            <div className="flex flex-wrap gap-2">{templateTokens.map(token => <code key={token} className="rounded bg-muted px-2 py-1 text-xs">{`{{${token}}}`}</code>)}</div>
          </div>
        </details>
  )
}
