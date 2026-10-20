import { useLocale } from "@/context/locale"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { fieldLabelKey, notificationFields, type ChannelField } from "@/lib/notification-fields"
import { channelFieldIsCleared, channelFieldPresent, type AlertChannelFormState } from "@/pages/admin/alert-channel-config"

export function NotificationProviderFields({ form, update, clear }: {
  form: AlertChannelFormState; update: (key: string, value: string) => void; clear: (path: string, selected: boolean) => void
}) {
  const { t } = useLocale()
  const fields = notificationFields[form.channelType]
  const primary = fields.filter(field => field.required || ["endpoint", "port", "security", "method", "bot_token", "device_key"].includes(field.key))
  const optional = fields.filter(field => !primary.includes(field))
  const props = { form, update, clear }
  return <div className="grid gap-4">
    {primary.map(field => <ProviderField key={field.key} field={field} {...props} />)}
    {!!optional.length && <details className="rounded-md border px-4 py-3">
      <summary className="cursor-pointer text-sm font-medium">{t("notification.optional_settings")}</summary>
      <div className="mt-4 grid gap-4">{optional.map(field => <ProviderField key={field.key} field={field} {...props} />)}</div>
    </details>}
    {Object.entries(form.present).filter(([key, exists]) => exists && (["auth", "hmac"].includes(key) || key.startsWith("named_secrets."))).map(([key]) => (
      <label key={key} className="flex items-center gap-2 text-xs text-muted-foreground">
        <Checkbox checked={form.clearSecrets.includes(key)} onCheckedChange={checked => clear(key, Boolean(checked))} />
        {t("notification.clear_saved", { field: key.startsWith("named_secrets.") ? key : t(fieldLabelKey(key)) })}
      </label>
    ))}
  </div>
}

function ProviderField({ field, form, update, clear }: {
  field: ChannelField; form: AlertChannelFormState; update: (key: string, value: string) => void; clear: (path: string, selected: boolean) => void
}) {
  const { t } = useLocale()
  const label = t(fieldLabelKey(field.key))
  const id = `notification-${field.key.replaceAll(".", "-")}`
  const value = form.values[field.key] ?? ""
  const saved = channelFieldPresent(form, field.key)
  const cleared = channelFieldIsCleared(form, field.key)
  const options = (field.choices ?? []).map(option => ({ value: option, label: option ? t(`notification.option.${option}`) : t("notification.option.default") }))
  let input
  if (field.kind === "choice") input = <Select items={options} value={value} onValueChange={next => update(field.key, next ?? "")} disabled={cleared}>
    <SelectTrigger id={id} className="w-full"><SelectValue /></SelectTrigger><SelectContent>
      {options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
    </SelectContent></Select>
  else if (field.kind === "boolean") input = <Checkbox id={id} checked={value === "true"} onCheckedChange={checked => update(field.key, String(Boolean(checked)))} />
  else if (["map", "body", "list"].includes(field.kind)) input = <Textarea id={id} rows={field.kind === "list" ? 2 : 4} autoComplete="off" spellCheck={false} value={value} disabled={cleared}
    placeholder={field.kind === "map" ? '{"key":"value"}' : undefined} onChange={event => update(field.key, event.target.value)} />
  else input = <Input id={id} type={field.kind === "secret" ? "password" : field.kind === "number" ? "number" : field.kind === "url" ? "url" : "text"}
    autoComplete={field.secret ? "new-password" : "off"} value={value} disabled={cleared} onChange={event => update(field.key, event.target.value)} />
  return <Field>
    <FieldLabel htmlFor={id}>{label}{field.required && <span aria-hidden="true"> *</span>}</FieldLabel>
    {input}
    {field.kind === "map" && <FieldDescription>{t("notification.map_hint")}</FieldDescription>}
    {field.kind === "list" && <FieldDescription>{t("notification.list_hint")}</FieldDescription>}
    {saved && field.secret && <FieldDescription>{t("notification.saved_hint")}</FieldDescription>}
    {saved && field.secret && field.key !== "named_secrets" && <label className="flex items-center gap-2 text-xs text-muted-foreground">
      <Checkbox checked={form.clearSecrets.includes(field.key)} onCheckedChange={checked => clear(field.key, Boolean(checked))} />
      {t("notification.clear_saved", { field: label })}
    </label>}
  </Field>
}
