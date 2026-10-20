import { useCallback, useEffect, useState } from "react"
import { useApiList } from "@/hooks/use-api-list"
import { useNotificationOwnerAction } from "@/hooks/use-notification-owner-action"
import { getNotificationSettings, saveNotificationSettings } from "@/lib/notification-api"
import { notificationSettingsError, notificationSettingsForm, notificationSettingsPayload, type NotificationSettingsForm } from "@/lib/notification-settings"
import { useLocale } from "@/context/locale"

export function useNotificationSettings() {
  const { t } = useLocale()
  const fetch = useCallback(async (signal: AbortSignal) => [await getNotificationSettings(signal)], [])
  const list = useApiList(fetch)
  const owner = useNotificationOwnerAction()
  const [form, setForm] = useState<NotificationSettingsForm | null>(null)
  const [error, setError] = useState("")
  const [saved, setSaved] = useState(false)
  useEffect(() => { if (list.data?.[0]) setForm(notificationSettingsForm(list.data[0])) }, [list.data])
  const change = (update: Partial<NotificationSettingsForm>) => { setForm(current => current ? { ...current, ...update } : current); setSaved(false) }
  const save = () => {
    if (!form) return
    const invalid = notificationSettingsError(form)
    setError(invalid ?? "")
    if (invalid) return
    const payload = notificationSettingsPayload(form)
    owner.request({ title: t("notification.save_settings"), description: t("notification.settings_change_help"), execute: async () => {
      const updated = await saveNotificationSettings(payload)
      setForm(notificationSettingsForm(updated)); setSaved(true)
    } })
  }
  return { form, change, save, owner, saved, refresh: list.refresh, loading: list.loading, error: error || list.error || owner.error }
}
