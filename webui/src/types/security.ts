export type AdminRole = "owner" | "admin"

export interface AdminUser {
  id: string
  username: string
  display_name: string
  role: AdminRole
  enabled: boolean
  password_changed_at: string
  last_login_at: string | null
  totp_enabled_at: string | null
  created_at: string
  updated_at: string
}

export interface AdminSession {
  id: string
  user_id: string
  token_prefix: string
  created_at: string
  last_activity_at: string
  idle_expires_at: string
  absolute_expires_at: string
  reauthenticated_until: string | null
  ip_address: string
  user_agent: string
  revoked_at: string | null
  authentication_method?: "local" | "oidc"
  external_identity_id?: string | null
  current?: boolean
}

export interface AdminPrincipal {
  user: AdminUser
  session: AdminSession
}

export interface AuthState {
  setup_required: boolean
}

export interface TOTPSetup {
  secret: string
  uri: string
}

export interface AdminAuditEvent {
  id: string
  request_id: string
  actor_type: "admin" | "service"
  actor_id: string
  actor_username: string
  action: string
  resource_type: string
  resource_id: string
  outcome: "success" | "failure"
  detail_json: string
  ip_address: string
  user_agent: string
  session_prefix: string
  created_at: string
}

/** 审计历史一律服务端筛选与游标分页；不再暴露总数与偏移。 */
export interface AuditPage {
  events: AdminAuditEvent[]
  next_cursor?: string
}

export interface OIDCProviderPublic {
  id: string
  slug: string
  display_name: string
}

export interface OIDCProvider {
  id: string
  slug: string
  display_name: string
  issuer_url: string
  client_id: string
  client_secret_configured: boolean
  scopes: string[]
  enabled: boolean
  configuration_version: number
  sort_order: number
  created_at: string
  updated_at: string
}

export interface ExternalIdentity {
  id: string
  provider_id: string
  email: string
  email_verified: boolean
  preferred_username?: string
  display_name?: string
  last_login_at: string
  created_at: string
  updated_at: string
  provider_name?: string
}

export type SecurityDeliveryState = "pending" | "delivered" | "failed" | "none"

export interface SecurityOption {
  value: string
  label: string
}

export interface SecurityOptions {
  kinds: SecurityOption[]
  severities: SecurityOption[]
  reasons: SecurityOption[]
  sources: SecurityOption[]
  deliveries: SecurityOption[]
  routing_states: SecurityOption[]
  privacy_levels: SecurityOption[]
}

export interface SecurityEventDelivery {
  delivery_id: string
  channel_id: string
  channel_name: string
  state: string
  attempt_count: number
  error_code?: string
  updated_at: string
}

export interface SecurityEvent {
  id: string
  kind: string
  severity: string
  actor_user_id: string
  actor_display_name: string
  auth_method: string
  source_class: string
  source_family: string
  source_summary: string
  source_kind: string
  browser_family: string
  os_family: string
  device_class: string
  device_summary: string
  country_code: string
  region: string
  reasons: string[]
  policy_revision: number
  routing_state: string
  audit_event_id: string
  message_id: string
  occurred_at: string
  retain_until: string
  acknowledged_at?: string
  acknowledged_by?: string
  acknowledged_note?: string
  delivery_state: string
  delivery_channels: number
  delivery_failures: number
  delivery_pending: number
  deliveries?: SecurityEventDelivery[]
}

export interface SecurityEventPage {
  events: SecurityEvent[]
  total: number
  limit: number
  offset: number
}

export interface SecurityEventFilter {
  kind?: string
  severity?: string
  reason?: string
  source?: string
  routing?: string
  delivery?: string
  actor?: string
  limit?: number
  offset?: number
}

export interface SecurityNotificationSettings {
  enabled_event_kinds: string[]
  privacy_level: string
  lookback_days: number
  threshold_failures: number
  threshold_window_seconds: number
  invalid_totp_threshold: number
  retention_days: number
  revision: number
  updated_at: string
  updated_by: string
}

export interface SecurityNotificationSettingsWrite {
  expected_revision: number
  enabled_event_kinds: string[]
  privacy_level: string
  lookback_days: number
  threshold_failures: number
  threshold_window_seconds: number
  invalid_totp_threshold: number
  retention_days: number
}

export interface SecurityEventSource {
  event_id: string
  address: string
  captured_at: string
}
