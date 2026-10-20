import { APIError } from "@/lib/api-error"
import { operationMutationError } from "./operation-mutation-error"
import { expect, it } from "vitest"
import { operationPolicy } from "@/test/operations-fixtures"
import { operationInputNumber, operationInteger, operationMaximumFileBytes, operationRetentionLimits, validOperationPolicy, validOperationRoot } from "./operation-policy-validation"
it("matches backend integer, root and bounded-default retention rules", () => {
 expect(Number.isNaN(operationInputNumber(""))).toBe(true); expect(operationInputNumber("2.5")).toBe(2.5)
 expect(operationInteger(0, 1, 0)).toBe(true); expect(operationInteger(1.5, 4)).toBe(false)
 expect(operationRetentionLimits(undefined)).toEqual({ task_days: 30, output_days: 7, artifact_hours: 24, queue_hours: 24, upload_hours: 6 })
 expect(operationRetentionLimits({ task_days: 2, output_days: 0, artifact_hours: 0, queue_hours: 0, upload_hours: 0 }).output_days).toBe(2)
 expect(operationRetentionLimits({ task_days: 0 }).task_days).toBe(30)
 const root = operationPolicy().policy.roots[0]
 expect(validOperationRoot({ ...root, maximum_bytes: 1 })).toBe(true)
 for (const patch of [{ id: "/etc" }, { id: "../host" }, { id: "" }, { id: "a".repeat(129) }, { maximum_bytes: 0 }, { maximum_bytes: 1.5 }, { maximum_bytes: operationMaximumFileBytes + 1 }, { name: "\u0001bad" }, { name: "字".repeat(81) }]) expect(validOperationRoot({ ...root, ...patch })).toBe(false)
 expect(validOperationRoot({ ...root, name: undefined })).toBe(true)
})
it("rejects unsafe policy and scope values before a request can be confirmed", () => {
 expect(validOperationPolicy(operationPolicy())).toBe(true)
 for (const patch of [{ name: "" }, { name: "字".repeat(70) }, { node_ids: [], group_ids: [] }, { node_ids: ["one", "one"] }, { group_ids: ["one", "one"] }, { node_ids: Array.from({ length: 201 }, (_, i) => String(i)) }]) expect(validOperationPolicy({ ...operationPolicy(), ...patch })).toBe(false)
 const invalid = [{ maximum_targets: 0 }, { maximum_targets: 201 }, { maximum_active_tasks: 1001 }, { concurrency: 5 }, { timeout_seconds: 1.5 }, { output_bytes: Number.NaN }, { kinds: [] }, { kinds: ["shell", "shell"] }, { kinds: ["unknown"] }, { supported_shells: ["bash"] }, { supported_shells: ["sh", "sh"] }, { roots: Array(65).fill(operationPolicy().policy.roots[0]) }, { roots: Array(2).fill(operationPolicy().policy.roots[0]) }, { roots: [{ ...operationPolicy().policy.roots[0], id: "/bad" }] }, { retention: { task_days: 91 } }, { retention: { task_days: 2, output_days: 3 } }, { retention: { artifact_hours: 25 } }, { retention: { queue_hours: -1 } }, { retention: { upload_hours: 7 } }]
 for (const patch of invalid) expect(validOperationPolicy({ ...operationPolicy(), policy: { ...operationPolicy().policy, ...patch } })).toBe(false)
})

it("provides actionable CAS errors without hiding storage or network failures", () => {
 const unavailable = new APIError("storage unavailable", 503)
 expect(operationMutationError(unavailable, "reload")).toBe(unavailable)
 const other = new Error("offline")
 expect(operationMutationError(other, "reload")).toBe(other)
 expect((operationMutationError(new APIError("operation request was rejected", 409), "reload current version") as Error).message).toBe("reload current version")
})
