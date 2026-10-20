import { acceleratorDeviceName } from "@/lib/accelerator-public"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
import type { DashboardDefinition, DashboardPanel, MetricDefinition } from "@/types/metric-workspace"
export function acceleratorDefinition(input: { nodeID: string; device?: AcceleratorDevicePublic; catalog: MetricDefinition[]; t: (key: string) => string }): DashboardDefinition {
 const definition: DashboardDefinition = { version: 1, title: "GPU", description: "", time_range_seconds: 3600, refresh_seconds: 30, variables: [], panels: [] }
 const groups = [
  { id: "utilization", title: input.t("historyUtilization"), metrics: ["gpu_utilization_percent"] },
  { id: "memory", title: input.t("historyMemory"), metrics: ["gpu_memory_used_bytes", "gpu_memory_total_bytes"] },
  { id: "temperature", title: input.t("historyTemperature"), metrics: ["gpu_temperature_celsius"] },
  { id: "power", title: input.t("historyPower"), metrics: ["gpu_power_watts", "gpu_power_limit_watts"] },
 ]
 if (!input.device) return definition
 const device = input.device
 definition.panels = groups.flatMap(group => {
  const metrics = group.metrics.map(key => input.catalog.find(metric => metric.key === key && metric.enabled && metric.public && device.capabilities.includes(key))).filter((metric): metric is MetricDefinition => Boolean(metric))
  if (!metrics.length || new Set(metrics.map(metric => metric.unit)).size > 1) return []
  const panel: DashboardPanel = { id: `gpu-${group.id}`, title: group.title, type: "timeseries", size: "medium", legend: true, unit: metrics[0].unit, thresholds: [], series: metrics.map((metric, index) => ({ ref_id: `GPU_${index}`, node_id: input.nodeID, device_id: device.device_id, metric: metric.key, aggregation: metric.default_aggregation, label: `${metric.label} · ${acceleratorDeviceName(device)}`, color: index ? "violet" : "blue" })) }
  return [panel]
 })
 return definition
}
