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

export interface AuditPage {
  events: AdminAuditEvent[]
  total: number
  limit: number
  offset: number
}
