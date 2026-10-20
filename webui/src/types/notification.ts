export type NotificationChannelType = "webhook" | "telegram" | "email" | "bark" | "serverchan_turbo" | "serverchan_3" | "custom_http"
export type DeliveryState = "pending" | "leased" | "succeeded" | "dead" | "cancelled"

export interface NotificationChannel {
  id: string
  name: string
  channel_type: NotificationChannelType
  enabled: boolean
  current_revision: number
  event_kinds: string[]
  config: Record<string, unknown>
  secret_present: Record<string, boolean>
  template_subject: string
  template_text: string
  health: ChannelHealth
  created_at: string
  updated_at: string
}

export interface ChannelHealth {
  revision: number
  consecutive_failures: number
  circuit: string
  open_until?: string
  last_success?: string
  last_failure?: string
  error_code?: string
  error_stage?: string
  queue_depth: number
}

export interface NotificationChannelWrite {
  name: string
  channel_type: NotificationChannelType
  enabled: boolean
  expected_revision: number
  event_kinds: string[]
  config: Record<string, unknown>
  clear_secrets?: string[]
  template_subject: string
  template_text: string
}

export interface DeliveryRef {
  id: string
  channel_id: string
  channel_revision: number
  state: DeliveryState
  attempt_count: number
}

export interface NotificationReceipt {
  message_id: string
  routing_state: string
  state: DeliveryState | "not_routed"
  counts: Record<DeliveryState, number>
  deliveries: DeliveryRef[]
  duplicate: boolean
  skipped: boolean
}

export interface NotificationDelivery extends DeliveryRef {
  message_id: string
  channel_name: string
  channel_type: NotificationChannelType
  event_kind: string
  source_kind: string
  source_id: string
  next_attempt_at: string
  error_code?: string
  error_stage?: string
  status_class?: string
  retry_of?: string
  created_at: string
  completed_at?: string
  attempts?: NotificationAttempt[]
}

export interface NotificationAttempt {
  number: number
  started_at: string
  completed_at?: string
  duration_ms: number
  outcome: string
  error_code?: string
  error_stage?: string
  status_class?: string
}

export interface DeliveryPage { items: NotificationDelivery[]; total: number; offset: number; limit: number }
export interface DeliveryFilter { state?: DeliveryState; channel_id?: string; offset?: number; limit?: number }

export interface NotificationEgressPolicy { revision: number; exceptions: EgressException[] }
export interface NotificationSettings {
  revision: number
  success_retention_days: number
  dead_retention_days: number
  maximum_workers: number
  egress_policy: NotificationEgressPolicy
}
export interface NotificationPreview {
  method: string
  url: string
  headers: Record<string, string>
  body: string
  content_type: string
  subject: string
  text: string
}
export interface EgressException {
  host?: string
  port: number
  cidr?: string
  purpose: string
  expires_at: string
  created_by?: string
  created_at?: string
}
