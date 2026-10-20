import MockAdapter from "axios-mock-adapter"
import { afterEach, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import * as evaluation from "@/lib/alert-evaluation-api"
import { evaluationDraft, evaluationError, evaluationWrite } from "@/lib/alert-evaluation-form"
import { evaluationDetailFixture, evaluationRuleFixture, evaluationSourcesFixture, evaluationStateFixture } from "@/test/alert-evaluation-fixtures"

const mock = new MockAdapter(api)
afterEach(() => mock.reset())
it("uses revisions, safe identifiers, bounded queries and cancellable read-only previews", async () => {
  const signal = new AbortController().signal, rule = evaluationRuleFixture({ id: "r/1" }), write = evaluationWrite(evaluationDraft(rule)), sources = evaluationSourcesFixture()
  mock.onGet("/admin/metrics/catalog").reply(200, { metrics: sources.metrics }); mock.onGet("/nodes/manage").reply(200, sources.nodes); mock.onGet("/groups").reply(200, sources.groups)
  expect(await evaluation.evaluationSources(signal)).toEqual(sources)
  mock.onGet("/alerts/rules").reply(200, [rule]); expect(await evaluation.evaluationRules(signal)).toEqual([rule])
  mock.onPost("/alerts/rules").reply(201, rule); await evaluation.saveEvaluationRule(write, null)
  mock.onPut("/alerts/rules/r%2F1").reply(200, rule); await evaluation.saveEvaluationRule(write, rule)
  expect(JSON.parse(mock.history.put[0].data)).toEqual({ ...write, expected_revision: 3 })
  mock.onDelete("/alerts/rules/r%2F1").reply(204); await evaluation.deleteEvaluationRule(rule)
  expect(JSON.parse(mock.history.delete[0].data)).toEqual({ expected_revision: 3 })
  mock.onGet("/alerts/evaluations?limit=50").reply(200, { items: [evaluationStateFixture()] }); expect((await evaluation.evaluationStates("limit=50", signal)).items).toHaveLength(1)
  mock.onGet("/alerts/evaluation-events?kind=error").reply(200, { items: [] }); expect((await evaluation.evaluationEvents("kind=error", signal)).items).toEqual([])
  mock.onGet("/alerts/evaluations/r%2F1/n%2F1").reply(200, evaluationDetailFixture()); expect(await evaluation.evaluationDetail("r/1", "n/1", signal)).toEqual(evaluationDetailFixture())
  mock.onPost("/alerts/rules/preview").reply(200, [{ node_id: "node-one", value: null }]); expect((await evaluation.previewEvaluation(write, ["node-one"], signal))[0].value).toBeNull()
  expect(mock.history.post.at(-1)?.signal).toBe(signal)
})
it("preserves nullable recovery and refuses blank, fractional and oversized policy numbers", () => {
  const draft = evaluationDraft()
  expect(evaluationWrite({ ...draft, name: "valid" }).recovery_threshold).toBeNull()
  expect(evaluationDraft(evaluationRuleFixture({ recovery_threshold: 70 })).recovery_threshold).toBe("70")
  for (const invalid of [{ threshold: "" }, { threshold: "Infinity" }, { duration: "1.2" }, { duration: "86401" }, { window_seconds: "1" }, { recovery_threshold: "NaN" }, { name: "" }, { scope: { kind: "nodes" as const, node_ids: [] } }, { scope: { kind: "groups" as const, group_ids: [] } }]) expect(() => evaluationWrite({ ...draft, name: "valid", ...invalid })).toThrow("invalid")
  expect(evaluationError(new Error("stale_revision"))).toBe("conflict"); expect(evaluationError(new Error("invalid_policy"))).toBe("invalid"); expect(evaluationError(new Error("active_incident_policy"))).toBe("locked"); expect(evaluationError(null)).toBe("Unknown error")
})
