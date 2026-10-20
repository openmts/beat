import { expect, it } from "vitest"
import { evaluationFiltersFromQuery, evaluationHistoryLink } from "@/lib/alert-evaluation-links"

it("links a rule to the filtered evaluation history tab", () => {
  expect(evaluationHistoryLink("rule-one")).toBe("/admin/alerts?tab=evaluation-history&rule_id=rule-one")
})

it("drops unusable rule ids instead of building a broken link", () => {
  expect(evaluationHistoryLink("rule&tab=channels")).toBe("/admin/alerts?tab=evaluation-history")
  expect(evaluationHistoryLink("")).toBe("/admin/alerts?tab=evaluation-history")
})

it("keeps only whitelisted deep-link filters", () => {
  const search = new URLSearchParams("rule_id=rule-one&node_id=node%20two&kind=recovered&status=error&phase=unknown")
  expect(evaluationFiltersFromQuery(search)).toEqual({ rule_id: "rule-one", node_id: "", phase: "", status: "error", kind: "recovered" })
})

it("returns empty filters when the address has no evaluation parameters", () => {
  expect(evaluationFiltersFromQuery(new URLSearchParams(""))).toEqual({ rule_id: "", node_id: "", phase: "", status: "", kind: "" })
})
