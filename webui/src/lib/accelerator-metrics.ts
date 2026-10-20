export const acceleratorMetricKeys = ["gpu_utilization_percent", "gpu_memory_used_bytes", "gpu_memory_total_bytes", "gpu_temperature_celsius", "gpu_power_watts", "gpu_power_limit_watts"] as const
export type AcceleratorMetricKey = typeof acceleratorMetricKeys[number]
export function isAcceleratorMetric(metric: string): metric is AcceleratorMetricKey { return acceleratorMetricKeys.includes(metric as AcceleratorMetricKey) }
const acceleratorMetricLabelKeys: Record<AcceleratorMetricKey, string> = {
 gpu_utilization_percent: "metric_gpu_utilization_percent", gpu_memory_used_bytes: "metric_gpu_memory_used_bytes",
 gpu_memory_total_bytes: "metric_gpu_memory_total_bytes", gpu_temperature_celsius: "metric_gpu_temperature_celsius",
 gpu_power_watts: "metric_gpu_power_watts", gpu_power_limit_watts: "metric_gpu_power_limit_watts",
}
/** 目录键只覆盖已知 GPU 指标，未知指标原样返回，避免把错误信息或自定义指标名加前缀。 */
export function acceleratorMetricLabel(metric: string, t: (key: string) => string): string {
 return isAcceleratorMetric(metric) ? t(acceleratorMetricLabelKeys[metric]) : metric
}
export function validAcceleratorValue(metric: string, value: unknown): value is number {
 if (typeof value !== "number" || !Number.isFinite(value)) return false
 if (metric === "gpu_utilization_percent") return value >= 0 && value <= 100
 if (metric === "gpu_temperature_celsius") return value >= -100 && value <= 250
 if (metric.includes("_bytes")) return isAcceleratorMetric(metric) && Number.isInteger(value) && value >= 0 && value <= 2 ** 50
 return isAcceleratorMetric(metric) && value >= 0 && value <= 1000000
}

export function acceleratorSeriesError(series: { metric: string; node_id: string; device_id?: string }): string | null {
 if (!isAcceleratorMetric(series.metric)) return series.device_id ? "workspace.invalid_panel" : null
 if (series.node_id.startsWith("$")) return "workspace.gpu_fixed_node"
 return series.device_id && /^[a-f0-9]{32}$/.test(series.device_id) ? null : "workspace.select_gpu_device"
}
