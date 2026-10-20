import { NotificationTemplateFields } from "@/pages/admin/notification-template-fields"
import type { Dispatch, SetStateAction } from "react"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { notificationChannelTypes, notificationEventKinds } from "@/lib/notification-fields"
import { emptyAlertChannelForm, type AlertChannelFormState } from "@/pages/admin/alert-channel-config"
import { NotificationProviderFields } from "@/pages/admin/notification-provider-fields"
import type { AlertChannelType } from "@/types"
import type { NotificationPreview } from "@/types/notification"


interface Props {
  open: boolean
  editing: boolean
  loading: boolean
  form: AlertChannelFormState
  setForm: Dispatch<SetStateAction<AlertChannelFormState>>
  onOpenChange: (open: boolean) => void
  onSave: () => void
  onPreview?: () => void
  error?: string | null
  preview?: NotificationPreview | null
}

export default function AlertChannelDialog(props: Props) {
  const { t } = useLocale()
  const { form, setForm } = props
  const types = notificationChannelTypes.map(value => ({ value, label: t(`notification.provider.${value}`) }))
  const update = (key: string, value: string) => setForm(current => ({ ...current, values: { ...current.values, [key]: value } }))
  const clear = (path: string, selected: boolean) => setForm(current => ({ ...current,
    clearSecrets: selected ? [...new Set([...current.clearSecrets, path])] : current.clearSecrets.filter(key => key !== path) }))
  const chooseType = (value: AlertChannelType) => setForm(current => ({ ...emptyAlertChannelForm(value), name: current.name, enabled: current.enabled }))
  const subscription = (kind: string, selected: boolean) => setForm(current => ({ ...current,
    eventKinds: selected ? [...new Set([...current.eventKinds, kind])] : current.eventKinds.filter(value => value !== kind) }))
  return <Dialog open={props.open} onOpenChange={open => { if (!props.loading) props.onOpenChange(open) }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader><DialogTitle>{t(props.editing ? "notification.edit_channel" : "notification.create_channel")}</DialogTitle>
        <DialogDescription>{t("notification.channel_help")}</DialogDescription></DialogHeader>
      <fieldset disabled={props.loading} className="grid min-w-0 gap-5">
        {props.error && <Alert variant="destructive" role="alert"><AlertDescription>{t(props.error)}</AlertDescription></Alert>}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field><FieldLabel htmlFor="channel-name">{t("node.name")}</FieldLabel><Input id="channel-name" maxLength={80} value={form.name} onChange={event => setForm(current => ({ ...current, name: event.target.value }))} /></Field>
          <Field><FieldLabel htmlFor="channel-type">{t("notification.provider")}</FieldLabel><Select items={types} value={form.channelType} disabled={props.editing} onValueChange={value => chooseType(value as AlertChannelType)}>
            <SelectTrigger id="channel-type" className="w-full"><SelectValue /></SelectTrigger><SelectContent>{types.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
          </Select></Field>
        </div>
        <label className="flex items-center justify-between gap-3 rounded-md border p-3 text-sm"><span>{t("notification.enabled_help")}</span>
          <Switch aria-label={t("alert.enabled")} checked={form.enabled} onCheckedChange={enabled => setForm(current => ({ ...current, enabled }))} /></label>
        <NotificationProviderFields form={form} update={update} clear={clear} />
        <fieldset className="rounded-md border p-4"><legend className="px-1 text-sm font-medium">{t("notification.subscriptions")}</legend>
          <p className="mb-3 text-xs text-muted-foreground">{t("notification.subscription_help")}</p>
          <div className="grid gap-3 sm:grid-cols-2">{notificationEventKinds.map(kind => <label key={kind} className="flex items-center gap-2 text-sm">
            <Checkbox checked={form.eventKinds.includes(kind)} onCheckedChange={checked => subscription(kind, Boolean(checked))} />{t(`notification.event.${kind}`)}
          </label>)}</div>
        </fieldset>
        <NotificationTemplateFields form={form} setForm={setForm} />
        {props.preview && <div className="grid gap-2 rounded-md border p-4"><p className="text-sm font-medium">{t("notification.preview_result")}</p>
          <p className="text-xs text-muted-foreground">{t("notification.preview_help")}</p>
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-3 text-xs">{JSON.stringify(props.preview, null, 2)}</pre>
        </div>}
      </fieldset>
      <DialogFooter><Button variant="outline" disabled={props.loading} onClick={() => props.onOpenChange(false)}>{t("app.cancel")}</Button>
        {props.onPreview && <Button variant="outline" disabled={props.loading} onClick={props.onPreview}>{t("notification.preview")}</Button>}
        <Button disabled={props.loading || !form.name.trim()} onClick={props.onSave}>{t("app.save")}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
