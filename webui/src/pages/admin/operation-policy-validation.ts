import type { OperationPolicy, OperationPolicyRecord, OperationRoot } from "@/types/operations"

export const operationMiB = 1024 * 1024
export const operationMaximumFileBytes = 2 * 1024 * operationMiB
export const operationMaximumRoots = 64
export const operationMaximumScope = 200
export const operationInteger = (value: number, maximum: number, minimum = 1) => Number.isSafeInteger(value) && value >= minimum && value <= maximum
export const operationInputNumber = (value: string) => value.trim() ? Number(value) : Number.NaN
export function operationRetentionLimits(value: OperationPolicy["retention"]) {
 const task_days = value?.task_days === undefined || value.task_days === 0 ? 30 : value.task_days
 return { task_days, output_days: value?.output_days === undefined || value.output_days === 0 ? Math.min(7, task_days) : value.output_days, artifact_hours: value?.artifact_hours === undefined || value.artifact_hours === 0 ? 24 : value.artifact_hours, queue_hours: value?.queue_hours === undefined || value.queue_hours === 0 ? 24 : value.queue_hours, upload_hours: value?.upload_hours === undefined || value.upload_hours === 0 ? 6 : value.upload_hours }
}
export function validOperationRoot(root: OperationRoot): boolean {
 return /^[A-Za-z0-9_-]{1,128}$/.test(root.id) && operationInteger(root.maximum_bytes, operationMaximumFileBytes) && [...(root.name ?? "")].length <= 80 && !/\p{Cc}/u.test(root.name ?? "")
}
export function validOperationPolicy(record: OperationPolicyRecord): boolean {
 const { policy, node_ids, group_ids } = record
 if (!record.name.trim() || new TextEncoder().encode(record.name).length > 200) return false
 if (!operationInteger(node_ids.length + group_ids.length, operationMaximumScope) || new Set(node_ids).size !== node_ids.length || new Set(group_ids).size !== group_ids.length) return false
 for (const [value, maximum] of [[policy.maximum_targets, 200], [policy.maximum_active_tasks, 1000], [policy.concurrency, 4], [policy.timeout_seconds, 3600], [policy.output_bytes, operationMiB]]) if (!operationInteger(value, maximum)) return false
 if (!policy.kinds.length || policy.kinds.length > 4 || new Set(policy.kinds).size !== policy.kinds.length || policy.kinds.some(kind => !["shell", "snippet", "deploy", "collect"].includes(kind))) return false
 const shells = policy.supported_shells ?? []
 if (new Set(shells).size !== shells.length || shells.some(shell => !["sh", "powershell"].includes(shell))) return false
 if (policy.roots.length > operationMaximumRoots || new Set(policy.roots.map(root => root.id)).size !== policy.roots.length || policy.roots.some(root => !validOperationRoot(root))) return false
 const retention = operationRetentionLimits(policy.retention)
 return operationInteger(retention.task_days, 90) && operationInteger(retention.output_days, Math.min(30, retention.task_days)) && operationInteger(retention.artifact_hours, 24) && operationInteger(retention.queue_hours, 24) && operationInteger(retention.upload_hours, 6)
}
