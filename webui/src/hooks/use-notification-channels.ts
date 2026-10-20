import { useRef, useState, type SetStateAction } from "react"
import { useAlertChannels } from "@/hooks/use-api"
import { useNotificationOwnerAction } from "@/hooks/use-notification-owner-action"
import { useReauthRetry } from "@/hooks/use-reauth-retry"
import * as api from "@/lib/api"
import { previewNotificationChannel } from "@/lib/notification-api"
import { useLocale } from "@/context/locale"
import type { AlertChannel } from "@/types"
import type { NotificationPreview } from "@/types/notification"
import { alertChannelFormError, alertChannelFormFrom, alertChannelPayload, emptyAlertChannelForm, type AlertChannelFormState } from "@/pages/admin/alert-channel-config"

export function useNotificationChannels() {
  const list = useAlertChannels()
  const { t } = useLocale()
  const owner = useNotificationOwnerAction()
  const [editing, setEditing] = useState<AlertChannel | null>(null)
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState(emptyAlertChannelForm)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState<NotificationPreview | null>(null)
  const running = useRef(false)
  const disabled = busy || owner.busy
  const edit = (channel?: AlertChannel) => {
    setEditing(channel ?? null); setForm(channel ? alertChannelFormFrom(channel) : emptyAlertChannelForm())
    setError(""); setPreview(null); setOpen(true)
  }
  const restarted = useReauthRetry()
  const operation = async (action: () => Promise<void>) => {
    if (running.current) return
    running.current = true; setBusy(true); setError(""); setNotice("")
    try { await restarted.run(async () => { await action(); await list.refresh() }) }
    catch (error) { setError(error instanceof Error ? error.message : "notification.action_failed") }
    finally { running.current = false; setBusy(false) }
  }
  const save = (onlyPreview = false) => {
    const invalid = alertChannelFormError(form)
    if (invalid) { setError(invalid); return }
    const payload = alertChannelPayload(form)
    owner.request({ title: t(onlyPreview ? "notification.preview" : "notification.save_channel"), description: t("notification.channel_change_help"),
      execute: async () => {
        if (onlyPreview) { setPreview(await previewNotificationChannel(payload, editing?.id)); return }
        if (editing) await api.updateAlertChannel(editing.id, payload)
        else await api.createAlertChannel(payload)
        setOpen(false); setForm(emptyAlertChannelForm()); setPreview(null); setNotice(t("notification.saved")); await list.refresh()
      } })
  }
  const remove = (channel: AlertChannel) => owner.request({ title: t("notification.delete_channel"),
    description: t("notification.delete_help", { name: channel.name }), confirm: true,
    execute: async () => { await api.deleteAlertChannel(channel.id); setNotice(t("notification.deleted")); await list.refresh() } })
  const test = (channel: AlertChannel) => operation(async () => {
    const receipt = await api.testAlertChannel(channel.id)
    setNotice(t("notification.test_queued", { state: t(`notification.state.${receipt.state}`) }))
  })
  const changeForm = (change: SetStateAction<AlertChannelFormState>) => { setForm(change); setPreview(null) }
  return { list, owner, editing, open, setOpen, form, setForm: changeForm, error, notice, preview, setPreview, disabled, edit, operation, save, remove, test, reauth: restarted }
}
