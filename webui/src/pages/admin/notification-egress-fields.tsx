import { Button } from "@/components/ui/button"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/context/locale"
import { newNotificationException, type ExceptionDraft } from "@/lib/notification-settings"

export function NotificationEgressFields({ entries, onChange, disabled }: { entries: ExceptionDraft[]; onChange: (entries: ExceptionDraft[]) => void; disabled: boolean }) {
  const { t } = useLocale()
  const update = (index: number, change: Partial<ExceptionDraft>) => onChange(entries.map((entry, at) => at === index ? { ...entry, ...change } : entry))
  const modes = [{ value: "host", label: t("notification.exact_host") }, { value: "cidr", label: t("notification.narrow_cidr") }]
  return <fieldset disabled={disabled} className="grid gap-4">
    <p className="max-w-3xl text-sm text-muted-foreground">{t("notification.egress_help")}</p>
    {entries.map((entry, index) => <div key={index} className="grid gap-3 rounded-md border p-4 sm:grid-cols-2">
      <Field><FieldLabel htmlFor={`egress-mode-${index}`}>{t("notification.target_kind")}</FieldLabel>
        <Select items={modes} value={entry.mode} onValueChange={mode => update(index, { mode: mode as ExceptionDraft["mode"] })} disabled={disabled}>
          <SelectTrigger id={`egress-mode-${index}`} className="w-full"><SelectValue /></SelectTrigger><SelectContent>{modes.map(mode => <SelectItem key={mode.value} value={mode.value}>{mode.label}</SelectItem>)}</SelectContent>
        </Select></Field>
      <Field><FieldLabel htmlFor={`egress-target-${index}`}>{t("notification.target")}</FieldLabel><Input id={`egress-target-${index}`} value={entry.target} onChange={event => update(index, { target: event.target.value })} /></Field>
      <Field><FieldLabel htmlFor={`egress-port-${index}`}>{t("node.port")}</FieldLabel><Input id={`egress-port-${index}`} type="number" min={1} max={65_535} value={entry.port} onChange={event => update(index, { port: event.target.value })} /></Field>
      <Field><FieldLabel htmlFor={`egress-expires-${index}`}>{t("notification.expires")}</FieldLabel><Input id={`egress-expires-${index}`} type="datetime-local" value={entry.expires} onChange={event => update(index, { expires: event.target.value })} /></Field>
      <Field className="sm:col-span-2"><FieldLabel htmlFor={`egress-purpose-${index}`}>{t("notification.purpose")}</FieldLabel><Input id={`egress-purpose-${index}`} value={entry.purpose} onChange={event => update(index, { purpose: event.target.value })} /></Field>
      <Button variant="outline" className="justify-self-start" onClick={() => onChange(entries.filter((_, at) => at !== index))}>{t("notification.remove_exception")}</Button>
    </div>)}
    {!entries.length && <p className="text-sm text-muted-foreground">{t("notification.no_exceptions")}</p>}
    {!disabled && <Button variant="outline" className="justify-self-start" disabled={entries.length >= 64} onClick={() => onChange([...entries, newNotificationException()])}>{t("notification.add_exception")}</Button>}
  </fieldset>
}
