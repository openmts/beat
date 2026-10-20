import { expect, it } from "vitest"
import { evaluationCopyName, evaluationCopySeed } from "@/lib/alert-evaluation-form"
import { evaluationRuleFixture } from "@/test/alert-evaluation-fixtures"

it("names the copy per locale and keeps the policy in the seed", () => {
  const seed = evaluationCopySeed(evaluationRuleFixture({ scope: { kind: "nodes", node_ids: ["node-one"] } }), "（副本）")
  expect(seed.name).toBe("CPU high（副本）")
  expect(seed.enabled).toBe(false)
  expect(seed.scope).toEqual({ kind: "nodes", node_ids: ["node-one"] })
  expect(evaluationCopyName("CPU high", " (copy)")).toBe("CPU high (copy)")
})

it("truncates a long rule name before appending the copy suffix", () => {
  const name = evaluationCopyName("x".repeat(200), " (copy)")
  expect(name).toHaveLength(128)
  expect(name.endsWith(" (copy)")).toBe(true)
})
