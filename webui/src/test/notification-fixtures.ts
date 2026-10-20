import type { NotificationChannel, NotificationDelivery, NotificationReceipt, NotificationSettings } from "@/types/notification"

export function notificationChannel(overrides: Partial<NotificationChannel> = {}): NotificationChannel {
  return { id: "c1", name: "Operations", channel_type: "webhook", enabled: true, current_revision: 3,
    event_kinds: ["alert.triggered", "traffic.report"], config: { endpoint_display: "https://[configured-host]" },
    secret_present: { endpoint: true }, template_subject: "", template_text: "",
    health: { revision: 3, circuit: "closed", consecutive_failures: 0, queue_depth: 1 },
    created_at: "2026-09-13T00:00:00Z", updated_at: "2026-09-13T00:00:00Z", ...overrides }
}

export function notificationReceipt(overrides: Partial<NotificationReceipt> = {}): NotificationReceipt {
  return { message_id: "message", routing_state: "routed", state: "pending",
    counts: { pending: 1, leased: 0, succeeded: 0, dead: 0, cancelled: 0 },
    deliveries: [{ id: "delivery", channel_id: "c1", channel_revision: 3, state: "pending", attempt_count: 0 }], duplicate: false, skipped: false, ...overrides }
}

export function notificationDelivery(overrides: Partial<NotificationDelivery> = {}): NotificationDelivery {
  return { id: "d1", message_id: "m1", channel_id: "c1", channel_name: "Operations", channel_type: "webhook", channel_revision: 3,
    state: "pending", attempt_count: 0, event_kind: "alert.triggered", source_kind: "alert", source_id: "event",
    next_attempt_at: "2026-09-13T00:05:00Z", created_at: "2026-09-13T00:00:00Z", ...overrides }
}

export function notificationSettings(): NotificationSettings {
  return { revision: 1, success_retention_days: 30, dead_retention_days: 90, maximum_workers: 8, egress_policy: { revision: 1, exceptions: [] } }
}
