import type { AlertRule } from "@/types"
import type { NotificationReceipt } from "@/types/notification"

export interface EvaluationScope { device_id?: string; kind: "all" | "nodes" | "groups"; node_ids?: string[]; group_ids?: string[] }
export interface EvaluationRule extends AlertRule {
  revision: number
  numeric_policy_locked: boolean
  scope: EvaluationScope
  window_seconds: number
  aggregation: string
  recovery_threshold: number | null
  recovery_duration: number
  no_data_policy: "keep" | "notify"
  no_data_delay_seconds: number
  error_policy: "keep" | "notify"
  error_delay_seconds: number
}
export type EvaluationWrite = Omit<EvaluationRule, "id" | "created_at" | "updated_at" | "revision" | "numeric_policy_locked">
export interface EvaluationHealth {
  epoch_id: string; status: string; started_at: string | null; status_since: string | null
  last_observed_at: string | null; elapsed_ms: number; suspended: boolean
}
export interface EvaluationState {
  device_id?: string; device_name?: string
  rule_id: string; node_id: string; rule_revision: number; state_version: number
  phase: string; evaluation_status: string; reason_code: string
  trigger_elapsed_ms: number; recovery_elapsed_ms: number
  pending_since: string | null; recovery_since: string | null; last_evaluated_at: string | null
  last_good_at: string | null; last_evidence_at: string | null; last_evidence_observed_at: string | null
  evidence_id: string; active_event_id: string; active_event_status: string; health: EvaluationHealth
  next_due_at: string; updated_at: string
}
export interface EvaluationEvent {
  device_id?: string; device_name?: string
  id: string; rule_id: string; node_id: string; rule_revision: number; epoch_id: string
  kind: string; reason_code: string; occurred_at: string; notified_at: string | null
  closed_at: string | null; source_key: string
}
export interface EvaluationPage<T> { items: T[]; next_cursor?: string }
export interface EvaluationDetail {
  state: EvaluationState; rule: EvaluationRule | null
  window: { from: string; to: string; aggregation: string; rule_revision: number } | null
  recent_health_events: EvaluationEvent[]
  notifications: Array<{ source_key: string; status: string; receipt?: NotificationReceipt }>
}
export interface EvaluationPreview { device_id?: string; device_name?: string; node_id: string; evaluation_status: string; reason_code: string; value: number | null; evidence_id?: string; source_at: string | null }
