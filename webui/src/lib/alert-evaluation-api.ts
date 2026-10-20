import { api } from "@/lib/api-client"
import type { Group, Node } from "@/types"
import type { MetricCatalog, MetricDefinition } from "@/types/metric-workspace"
import type { EvaluationRule, EvaluationWrite, EvaluationPage, EvaluationState, EvaluationEvent, EvaluationDetail, EvaluationPreview } from "@/types/alert-evaluation"

export interface EvaluationSources { metrics: MetricDefinition[]; nodes: Node[]; groups: Group[] }
export async function evaluationSources(signal: AbortSignal): Promise<EvaluationSources> {
  const [catalog, nodes, groups] = await Promise.all([api.get<MetricCatalog>("/admin/metrics/catalog", { signal }), api.get<Node[]>("/nodes/manage", { signal }), api.get<Group[]>("/groups", { signal })])
  return { metrics: catalog.data.metrics, nodes: nodes.data, groups: groups.data }
}
export async function evaluationRules(signal: AbortSignal): Promise<EvaluationRule[]> { return (await api.get<EvaluationRule[]>("/alerts/rules", { signal })).data }
export async function saveEvaluationRule(write: EvaluationWrite, previous: EvaluationRule | null): Promise<EvaluationRule> {
  if (previous) return (await api.put<EvaluationRule>(`/alerts/rules/${encodeURIComponent(previous.id)}`, { ...write, expected_revision: previous.revision })).data
  return (await api.post<EvaluationRule>("/alerts/rules", write)).data
}
export async function deleteEvaluationRule(rule: EvaluationRule): Promise<void> { await api.delete(`/alerts/rules/${encodeURIComponent(rule.id)}`, { data: { expected_revision: rule.revision } }) }
export async function evaluationStates(query: string, signal: AbortSignal): Promise<EvaluationPage<EvaluationState>> { return (await api.get<EvaluationPage<EvaluationState>>(`/alerts/evaluations?${query}`, { signal })).data }
export async function evaluationEvents(query: string, signal: AbortSignal): Promise<EvaluationPage<EvaluationEvent>> { return (await api.get<EvaluationPage<EvaluationEvent>>(`/alerts/evaluation-events?${query}`, { signal })).data }
export async function evaluationDetail(rule: string, node: string, signal: AbortSignal): Promise<EvaluationDetail> { return (await api.get<EvaluationDetail>(`/alerts/evaluations/${encodeURIComponent(rule)}/${encodeURIComponent(node)}`, { signal })).data }
export async function previewEvaluation(rule: EvaluationWrite, nodeIDs: string[], signal: AbortSignal): Promise<EvaluationPreview[]> { return (await api.post<EvaluationPreview[]>("/alerts/rules/preview", { rule, node_ids: nodeIDs }, { signal })).data }

export async function evaluationDeviceDetail(target: { ruleID: string; nodeID: string; deviceID: string }, signal: AbortSignal): Promise<EvaluationDetail> { return (await api.get<EvaluationDetail>(`/alerts/evaluations/${encodeURIComponent(target.ruleID)}/${encodeURIComponent(target.nodeID)}`, { signal, params: { device_id: target.deviceID } })).data }
