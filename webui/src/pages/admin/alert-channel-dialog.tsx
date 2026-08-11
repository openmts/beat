import type { Dispatch, SetStateAction } from "react"
import { LoaderCircleIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { AlertChannelType } from "@/types"
import type { AlertChannelFormState, EmailSecurity } from "@/pages/admin/alert-channel-config"

interface AlertChannelDialogProps {
  open: boolean
  editing: boolean
  loading: boolean
  form: AlertChannelFormState
  setForm: Dispatch<SetStateAction<AlertChannelFormState>>
  onOpenChange: (open: boolean) => void
  onSave: () => void
}

export default function AlertChannelDialog({
  open,
  editing,
  loading,
  form,
  setForm,
  onOpenChange,
  onSave,
}: AlertChannelDialogProps) {
  const { t } = useLocale()
  const typeItems = [
    { value: "webhook", label: t("alert.channel.webhook") },
    { value: "telegram", label: t("alert.channel.telegram") },
    { value: "email", label: t("alert.channel.email") },
  ]
  const securityItems = [
    { value: "starttls", label: "STARTTLS" },
    { value: "tls", label: "TLS" },
    { value: "none", label: t("alert.smtp_none") },
  ]
  const update = <K extends keyof AlertChannelFormState>(key: K, value: AlertChannelFormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }))
  }
  const updateSecurity = (value: EmailSecurity) => {
    setForm((current) => ({
      ...current,
      smtpSecurity: value,
      smtpPort: defaultPort(value, current.smtpPort),
    }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(90vh,48rem)] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {editing ? t("app.edit") : t("app.create")} {t("alert.channels")}
          </DialogTitle>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="channel-name">{t("node.name")}</FieldLabel>
            <Input
              id="channel-name"
              value={form.name}
              onChange={(event) => update("name", event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="channel-type">{t("app.type")}</FieldLabel>
            <Select
              items={typeItems}
              value={form.channelType}
              onValueChange={(value) => update("channelType", value as AlertChannelType)}
            >
              <SelectTrigger id="channel-type" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {typeItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          {form.channelType === "webhook" && (
            <Field>
              <FieldLabel htmlFor="webhook-url">{t("alert.webhook_url")}</FieldLabel>
              <Input
                id="webhook-url"
                type="url"
                value={form.webhookURL}
                placeholder="https://example.com/alerts"
                onChange={(event) => update("webhookURL", event.target.value)}
              />
            </Field>
          )}
          {form.channelType === "telegram" && (
            <>
              <Field>
                <FieldLabel htmlFor="telegram-token">{t("alert.telegram_token")}</FieldLabel>
                <Input
                  id="telegram-token"
                  type="password"
                  value={form.botToken}
                  autoComplete="new-password"
                  onChange={(event) => update("botToken", event.target.value)}
                />
                {editing && <FieldDescription>{t("alert.secret_unchanged")}</FieldDescription>}
              </Field>
              <Field>
                <FieldLabel htmlFor="telegram-chat">{t("alert.telegram_chat_id")}</FieldLabel>
                <Input
                  id="telegram-chat"
                  value={form.chatID}
                  onChange={(event) => update("chatID", event.target.value)}
                />
              </Field>
            </>
          )}
          {form.channelType === "email" && (
            <EmailFields
              editing={editing}
              form={form}
              securityItems={securityItems}
              update={update}
              updateSecurity={updateSecurity}
            />
          )}
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button onClick={onSave} disabled={loading || !form.name.trim()}>
            {loading && <LoaderCircleIcon data-icon="inline-start" className="animate-spin" />}
            {t("app.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface EmailFieldsProps {
  editing: boolean
  form: AlertChannelFormState
  securityItems: Array<{ value: string; label: string }>
  update: <K extends keyof AlertChannelFormState>(key: K, value: AlertChannelFormState[K]) => void
  updateSecurity: (value: EmailSecurity) => void
}

function EmailFields({ editing, form, securityItems, update, updateSecurity }: EmailFieldsProps) {
  const { t } = useLocale()
  return (
    <>
      <div className="grid grid-cols-[minmax(0,1fr)_7rem] gap-3">
        <Field>
          <FieldLabel htmlFor="smtp-host">{t("alert.smtp_host")}</FieldLabel>
          <Input id="smtp-host" value={form.smtpHost} onChange={(event) => update("smtpHost", event.target.value)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="smtp-port">{t("node.port")}</FieldLabel>
          <Input id="smtp-port" type="number" value={form.smtpPort} onChange={(event) => update("smtpPort", event.target.value)} />
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor="smtp-security">{t("alert.smtp_security")}</FieldLabel>
        <Select items={securityItems} value={form.smtpSecurity} onValueChange={(value) => updateSecurity(value as EmailSecurity)}>
          <SelectTrigger id="smtp-security" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent><SelectGroup>
            {securityItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectGroup></SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldLabel htmlFor="smtp-username">{t("alert.smtp_username")}</FieldLabel>
        <Input id="smtp-username" value={form.smtpUsername} onChange={(event) => update("smtpUsername", event.target.value)} />
      </Field>
      <Field>
        <FieldLabel htmlFor="smtp-password">{t("alert.smtp_password")}</FieldLabel>
        <Input id="smtp-password" type="password" value={form.smtpPassword} autoComplete="new-password" onChange={(event) => update("smtpPassword", event.target.value)} />
        {editing && <FieldDescription>{t("alert.secret_unchanged")}</FieldDescription>}
      </Field>
      <Field>
        <FieldLabel htmlFor="smtp-from">{t("alert.smtp_from")}</FieldLabel>
        <Input id="smtp-from" type="email" value={form.smtpFrom} onChange={(event) => update("smtpFrom", event.target.value)} />
      </Field>
      <Field>
        <FieldLabel htmlFor="smtp-to">{t("alert.smtp_to")}</FieldLabel>
        <Input id="smtp-to" value={form.smtpTo} placeholder="ops@example.com, owner@example.com" onChange={(event) => update("smtpTo", event.target.value)} />
      </Field>
    </>
  )
}

function defaultPort(security: EmailSecurity, current: string) {
  if (security === "tls" && (current === "587" || current === "25")) return "465"
  if (security === "starttls" && (current === "465" || current === "25")) return "587"
  if (security === "none" && (current === "465" || current === "587")) return "25"
  return current
}
