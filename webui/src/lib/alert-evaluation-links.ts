// 告警规则行可以跳到“评估历史”并带上该规则作为筛选条件；深链参数一律做白名单与边界校验，
// 避免把任意字符串带进查询，也避免 URL 改动被误解为需要重新渲染的页面状态。

export type EvaluationFilters = { rule_id: string; node_id: string; phase: string; status: string; kind: string }

const identifier = /^[A-Za-z0-9_-]{1,64}$/
const allowed = {
  phase: ["normal", "pending", "firing", "recovering"],
  status: ["ok", "no_data", "error", "disabled", "out_of_scope"],
  kind: ["no_data", "error", "recovered"],
} as const

function match(key: keyof typeof allowed, value: string | null): string {
  return (allowed[key] as readonly string[]).includes(value ?? "") ? value! : ""
}

function identifierOf(value: string | null): string {
  return identifier.test(value ?? "") ? value! : ""
}

/** 规则 → 该规则评估历史的深链；规则 id 只允许标识符字符。 */
export function evaluationHistoryLink(ruleID: string): string {
  const id = identifierOf(ruleID)
  return id ? `/admin/alerts?tab=evaluation-history&rule_id=${id}` : "/admin/alerts?tab=evaluation-history"
}

export function evaluationFiltersFromQuery(search: URLSearchParams): EvaluationFilters {
  return {
    rule_id: identifierOf(search.get("rule_id")),
    node_id: identifierOf(search.get("node_id")),
    phase: match("phase", search.get("phase")),
    status: match("status", search.get("status")),
    kind: match("kind", search.get("kind")),
  }
}
