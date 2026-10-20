import { workspaceNode } from "@/test/metric-workspace-fixtures"
import type { EvaluationSources } from "@/lib/alert-evaluation-api"
import type { EvaluationDetail, EvaluationRule, EvaluationState } from "@/types/alert-evaluation"

export function evaluationRuleFixture(overrides: Partial<EvaluationRule> = {}): EvaluationRule {
  return { id: "rule-one", name: "CPU high", description: "Keep evidence", metric: "cpu", operator: "gt", threshold: 80, duration: 60, severity: "warning", enabled: true, created_at: "", updated_at: "", revision: 3, numeric_policy_locked: false, scope: { kind: "all" }, window_seconds: 0, aggregation: "last", recovery_threshold: null, recovery_duration: 10, no_data_policy: "keep", no_data_delay_seconds: 300, error_policy: "keep", error_delay_seconds: 60, ...overrides }
}
export function evaluationSourcesFixture(): EvaluationSources {
  return { nodes: [{ ...workspaceNode("node-one"), name: "Alpha", group_id: "group-one" }, { ...workspaceNode("node-two"), name: "Beta", group_id: "group-two" }], groups: [{ id: "group-one", name: "Production", sort_order: 0, is_default: true, created_at: "", updated_at: "" }, { id: "group-two", name: "Staging", sort_order: 1, is_default: false, created_at: "", updated_at: "" }], metrics: [{ key: "cpu", label: "CPU", description: "", source: "builtin", kind: "gauge", unit: "percent", aggregations: ["avg", "max", "last"], default_aggregation: "avg", public: true, enabled: true, retention_days: 30, retention_override_days: null, revision: 1 }] }
}
export function evaluationStateFixture(overrides: Partial<EvaluationState> = {}): EvaluationState {
  return { rule_id: "rule-one", node_id: "node-one", rule_revision: 3, state_version: 2, phase: "firing", evaluation_status: "no_data", reason_code: "stale_value", trigger_elapsed_ms: 15000, recovery_elapsed_ms: 0, pending_since: null, recovery_since: null, last_evaluated_at: "2026-10-07T12:00:00Z", last_good_at: "2026-10-07T11:59:00Z", last_evidence_at: "2026-10-07T11:59:00Z", last_evidence_observed_at: "2026-10-07T11:59:00Z", evidence_id: "evidence", active_event_id: "incident", active_event_status: "acknowledged", health: { epoch_id: "epoch", status: "no_data", started_at: null, status_since: null, last_observed_at: null, elapsed_ms: 0, suspended: false }, next_due_at: "2026-10-07T12:00:30Z", updated_at: "2026-10-07T12:00:00Z", ...overrides }
}
export function evaluationDetailFixture(): EvaluationDetail { return { state: evaluationStateFixture(), rule: evaluationRuleFixture(), window: { from: "2026-10-07T11:58:00Z", to: "2026-10-07T11:59:00Z", aggregation: "avg", rule_revision: 3 }, recent_health_events: [], notifications: [{ source_key: "alert:incident:triggered", status: "history_unavailable" }] } }
