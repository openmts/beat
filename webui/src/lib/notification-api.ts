import { api } from "@/lib/api-client"
import type { DeliveryFilter, DeliveryPage, DeliveryRef, NotificationChannelWrite, NotificationDelivery, NotificationPreview, NotificationSettings } from "@/types/notification"

export async function setNotificationChannelEnabled(id: string, enabled: boolean) {
  const response = await api.patch(`/alerts/channels/${encodeURIComponent(id)}/enabled`, { enabled })
  return response.data
}

export async function listNotificationDeliveries(filter: DeliveryFilter, signal?: AbortSignal): Promise<DeliveryPage> {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filter)) if (value !== undefined && value !== "") query.set(key, String(value))
  const response = await api.get<DeliveryPage>(`/notifications/deliveries?${query}`, { signal })
  return response.data!
}

export async function getNotificationDelivery(id: string, signal?: AbortSignal): Promise<NotificationDelivery> {
  const response = await api.get<NotificationDelivery>(`/notifications/deliveries/${encodeURIComponent(id)}`, { signal })
  return response.data!
}

export async function retryNotificationDelivery(id: string): Promise<DeliveryRef> {
  const response = await api.post<DeliveryRef>(`/notifications/deliveries/${encodeURIComponent(id)}/retry`, { confirm_duplicate: true })
  return response.data!
}

export async function cancelNotificationDelivery(id: string): Promise<void> {
  await api.post(`/notifications/deliveries/${encodeURIComponent(id)}/cancel`)
}

export async function getNotificationSettings(signal?: AbortSignal): Promise<NotificationSettings> {
  const response = await api.get<NotificationSettings>("/notifications/settings", { signal })
  return response.data!
}

export async function saveNotificationSettings(settings: NotificationSettings): Promise<NotificationSettings> {
  const { revision, egress_policy, ...policy } = settings
  const response = await api.put<NotificationSettings>("/notifications/settings", {
    ...policy, expected_revision: revision, egress_policy: { exceptions: egress_policy.exceptions },
  })
  return response.data!
}

export async function getNotificationEventKinds(signal?: AbortSignal): Promise<string[]> {
  const response = await api.get<{ items: { kind: string; available: boolean }[] }>("/notifications/event-kinds", { signal })
  return response.data.items.filter(item => item.available).map(item => item.kind)
}

export async function previewNotificationChannel(channel: NotificationChannelWrite, id?: string): Promise<NotificationPreview> {
  const response = await api.post<NotificationPreview>("/alerts/channels/preview", {
    channel_id: id, expected_revision: channel.expected_revision, channel_type: channel.channel_type,
    config: channel.config, template_subject: channel.template_subject, template_text: channel.template_text,
    clear_secrets: channel.clear_secrets,
    subject: "Beat notification preview", text: "CPU usage is above the configured threshold.",
    data: { "event.kind": "alert.triggered", "event.severity": "warning", "event.status": "triggered", "event.message": "CPU usage is above the configured threshold.",
      "node.name": "edge-node", "rule.name": "CPU usage", "metric.name": "cpu", "metric.value": "92.5", "metric.formatted_value": "92.50" },
  })
  return response.data!
}
