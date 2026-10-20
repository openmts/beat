import { api } from "@/lib/api-client"
import type { AcceleratorPolicy, AcceleratorPolicyWrite, NodeAcceleratorsAdmin } from "@/types/accelerator"
export type AcceleratorScope = { kind: "node"; id: string } | { kind: "enrollment_policy" }
function policyPath(scope: AcceleratorScope) { return scope.kind === "node" ? `/admin/nodes/${encodeURIComponent(scope.id)}/accelerator-policy` : "/admin/accelerator-enrollment-policy" }
export async function getAcceleratorInventory(nodeID: string, signal: AbortSignal) { return (await api.get<NodeAcceleratorsAdmin>(`/admin/nodes/${encodeURIComponent(nodeID)}/accelerators`, { signal })).data }
export async function getAcceleratorPolicy(scope: AcceleratorScope, signal: AbortSignal) { return (await api.get<AcceleratorPolicy>(policyPath(scope), { signal })).data }
export async function updateAcceleratorPolicy(scope: AcceleratorScope, input: AcceleratorPolicyWrite) { return (await api.put<AcceleratorPolicy>(policyPath(scope), input)).data }

// updateAcceleratorVisibility 只修改指标页的节点展示开关，不改变采集策略。
export async function updateAcceleratorVisibility(nodeID: string, visible: boolean) {
  return (await api.put<{ node_id: string; visible: boolean }>(`/admin/nodes/${encodeURIComponent(nodeID)}/accelerator-visibility`, { visible })).data
}
