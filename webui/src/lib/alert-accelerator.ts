import { isAcceleratorMetric } from "@/lib/accelerator-metrics"
import type { EvaluationDraft } from "@/lib/alert-evaluation-form"
export const acceleratorRatioMetrics = ["gpu_memory_percent", "gpu_power_limit_percent"] as const
export function isAcceleratorAlertMetric(metric: string): boolean { return isAcceleratorMetric(metric) || acceleratorRatioMetrics.includes(metric as typeof acceleratorRatioMetrics[number]) }
export function acceleratorAlertInputs(metric: string): string[] {
 if (metric === "gpu_memory_percent") return ["gpu_memory_used_bytes", "gpu_memory_total_bytes"]
 if (metric === "gpu_power_limit_percent") return ["gpu_power_watts", "gpu_power_limit_watts"]
 return [metric]
}
export function evaluationMetricChange(draft: EvaluationDraft, metric: string): EvaluationDraft {
 const scope = { ...draft.scope }
 if (isAcceleratorAlertMetric(metric)) return { ...draft, metric, window_seconds: "0", aggregation: "last", scope: { kind: "nodes", node_ids: scope.node_ids?.slice(0, 1) ?? [], device_id: scope.device_id || "*" } }
 delete scope.device_id
 return { ...draft, metric, scope, window_seconds: "0", aggregation: "last" }
}
