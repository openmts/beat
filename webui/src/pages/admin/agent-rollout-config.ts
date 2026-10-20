import type { Translate } from "@/context/locales"
import type { AgentRollout, AgentRolloutCommand, AgentRolloutPolicy, AgentRolloutTarget } from "@/types/agent-rollouts"
export const defaultAgentRolloutPolicy: AgentRolloutPolicy = { canary_count: 1, wave_size: 10, max_concurrent: 1, observation_seconds: 300, report_freshness_seconds: 60, max_restarts: 0, max_protocol_errors: 0, max_metric_errors: 0, max_task_failures: 0, allow_preview: false, window_start: "", window_end: "" }
// 字段表第二列是文案键：渲染与校验共用一份清单，避免标签与错误里的措辞漂移。
export const fields = [
 ["canary_count", "agentRollout.field_canary_count", 1, 1000], ["wave_size", "agentRollout.field_wave_size", 1, 1000], ["max_concurrent", "agentRollout.field_max_concurrent", 1, 100],
 ["observation_seconds", "agentRollout.field_observation_seconds", 30, 3600], ["report_freshness_seconds", "agentRollout.field_report_freshness_seconds", 5, 3600],
 ["max_restarts", "agentRollout.field_max_restarts", 0, 1_000_000], ["max_protocol_errors", "agentRollout.field_max_protocol_errors", 0, 1_000_000], ["max_metric_errors", "agentRollout.field_max_metric_errors", 0, 1_000_000], ["max_task_failures", "agentRollout.field_max_task_failures", 0, 1_000_000],
] as const
export function validateAgentRolloutPolicy(policy: AgentRolloutPolicy, t: Translate) {
 for (const [key, labelKey, min, max] of fields) if (!Number.isInteger(policy[key]) || policy[key] < min || policy[key] > max) return t("agentRollout.error_range", { label: t(labelKey), minimum: min, maximum: max })
 const start = Date.parse(policy.window_start), end = Date.parse(policy.window_end)
 if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || end <= Date.now() || end - start > 30 * 24 * 60 * 60 * 1000) return t("agentRollout.error_window")
 return ""
}
export function agentObservationRemaining({ target, serverTime, seconds, t }: { target: AgentRolloutTarget; serverTime: string | undefined; seconds: number; t: Translate }) {
 if (!target.healthy_since || !serverTime) return t("agentRollout.observation_waiting")
 const elapsed = (Date.parse(serverTime) - Date.parse(target.healthy_since)) / 1000
 if (!Number.isFinite(elapsed) || elapsed < 0) return t("agentRollout.observation_unavailable")
 const remaining = Math.max(0, Math.ceil(seconds - elapsed))
 return remaining ? t("agentRollout.observation_remaining", { remaining }) : t("agentRollout.observation_elapsed")
}
export function canDeleteAgentRollout(value: AgentRollout) { return value.state === "completed" || value.state === "aborted" }
export function canCommandAgentRollout(value: AgentRollout, command: AgentRolloutCommand) {
 if (value.state === "completed" || value.state === "aborted") return false
 if (command === "resume") return value.state === "paused" && value.current_wave >= 0
 if (command === "promote") return value.state !== "paused" || value.reason === "awaiting_promotion"
 return command !== "pause" || value.state === "active"
}
