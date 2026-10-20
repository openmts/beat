// v24 应用历史保留策略前端契约；会话窗口是固定安全策略，不可配置。
export type ApplicationRetentionRunState = "never" | "running" | "success" | "failed"
export type ApplicationRetentionTrigger = "" | "manual" | "automatic"

export interface ApplicationRetentionSettings {
  alert_event_days: number
  audit_event_days: number
  cleanup_hour_utc: number
  auto_cleanup_enabled: boolean
  updated_at: string
}

export interface ApplicationRetentionStatus {
  running: boolean
  last_started_at: string | null
  last_completed_at: string | null
  last_status: ApplicationRetentionRunState
  last_error_code: string
  last_trigger: ApplicationRetentionTrigger
  last_duration_ms: number
  deleted_alert_events: number
  deleted_audit_events: number
  deleted_sessions: number
}

export interface ApplicationRetentionOverview {
  settings: ApplicationRetentionSettings
  status: ApplicationRetentionStatus
  /** 已过期会话的固定清理窗口，只读。 */
  session_retention_days: number
}
