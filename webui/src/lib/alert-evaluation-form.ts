import { isAcceleratorAlertMetric } from "@/lib/alert-accelerator"
import type { EvaluationRule, EvaluationWrite } from "@/types/alert-evaluation"

const numericFields = ["threshold", "duration", "window_seconds", "recovery_duration", "no_data_delay_seconds", "error_delay_seconds"] as const
export type NumericField = typeof numericFields[number]
export type EvaluationDraft = Omit<EvaluationWrite, NumericField | "recovery_threshold"> & Record<NumericField | "recovery_threshold", string>
/** seed 用于面板级告警等入口预填新建草稿；编辑既有规则时不传。 */
export function evaluationDraft(rule?: EvaluationRule, seed?: Partial<EvaluationWrite>): EvaluationDraft {
  const initial: EvaluationWrite = { ...(rule ?? { name: "", description: "", metric: "cpu", operator: "gt", threshold: 80, duration: 300, severity: "warning", enabled: true, scope: { kind: "all" }, window_seconds: 0, aggregation: "last", recovery_threshold: null, recovery_duration: 0, no_data_policy: "keep", no_data_delay_seconds: 300, error_policy: "keep", error_delay_seconds: 60 }), ...(rule ? undefined : seed) }
  return { name: initial.name, description: initial.description, metric: initial.metric, operator: initial.operator, severity: initial.severity, enabled: initial.enabled, scope: structuredClone(initial.scope), aggregation: initial.aggregation, no_data_policy: initial.no_data_policy, error_policy: initial.error_policy,
    threshold: String(initial.threshold), duration: String(initial.duration), window_seconds: String(initial.window_seconds), recovery_duration: String(initial.recovery_duration), no_data_delay_seconds: String(initial.no_data_delay_seconds), error_delay_seconds: String(initial.error_delay_seconds), recovery_threshold: initial.recovery_threshold == null ? "" : String(initial.recovery_threshold) }
}
export function evaluationWrite(draft: EvaluationDraft): EvaluationWrite {
  const parsed = {} as Record<NumericField, number>
  for (const field of numericFields) {
    const value = Number(draft[field])
    if (!draft[field].trim() || !Number.isFinite(value) || (field !== "threshold" && (!Number.isInteger(value) || value < 0 || value > 86400))) throw new Error("invalid")
    parsed[field] = value
  }
  const recovery = draft.recovery_threshold.trim() ? Number(draft.recovery_threshold) : null
  if (recovery !== null && !Number.isFinite(recovery)) throw new Error("invalid")
  if (!draft.name.trim() || (parsed.window_seconds > 0 && parsed.window_seconds < 10)) throw new Error("invalid")
  if (draft.scope.kind === "nodes" && !draft.scope.node_ids?.length || draft.scope.kind === "groups" && !draft.scope.group_ids?.length) throw new Error("invalid")
  if (isAcceleratorAlertMetric(draft.metric) && (draft.scope.kind !== "nodes" || draft.scope.node_ids?.length !== 1 || !(draft.scope.device_id === "*" || /^[a-f0-9]{32}$/.test(draft.scope.device_id ?? "")))) throw new Error("invalid")
  if (!isAcceleratorAlertMetric(draft.metric) && draft.scope.device_id) throw new Error("invalid")
  return { ...draft, ...parsed, recovery_threshold: recovery }
}
export function evaluationError(error: unknown): string {
  const message = error instanceof Error ? error.message : "Unknown error"
  return message === "stale_revision" ? "conflict" : message === "invalid_policy" ? "invalid" : message === "active_incident_policy" ? "locked" : message
}
/** 复制规则的默认名；超过后端 128 字符上限时先截断原名，再追加调用方给出的本地化后缀。 */
export function evaluationCopyName(name: string, suffix: string): string {
  return `${name.slice(0, 128 - suffix.length)}${suffix}`
}

/** 复制规则只复用策略字段，不带 id/revision，并以停用状态创建，避免复制后立刻重复通知。 */
export function evaluationCopySeed(rule: EvaluationRule, suffix: string): Partial<EvaluationWrite> {
  return { name: evaluationCopyName(rule.name, suffix), description: rule.description, metric: rule.metric, operator: rule.operator, threshold: rule.threshold,
    duration: rule.duration, severity: rule.severity, enabled: false, scope: structuredClone(rule.scope), window_seconds: rule.window_seconds, aggregation: rule.aggregation,
    recovery_threshold: rule.recovery_threshold, recovery_duration: rule.recovery_duration, no_data_policy: rule.no_data_policy, no_data_delay_seconds: rule.no_data_delay_seconds, error_policy: rule.error_policy, error_delay_seconds: rule.error_delay_seconds }
}

export const currentOnlyMetric = (metric: string) => metric === "heartbeat_age_seconds" || metric === "traffic_usage_percent"
