import { describe, expect, it } from "vitest"
import { acceleratorCatalog, acceleratorDevice, acceleratorInventory, acceleratorPolicy } from "@/test/accelerator-fixtures"
import { acceleratorMetricKeys, acceleratorMetricLabel, acceleratorSeriesError, validAcceleratorValue } from "./accelerator-metrics"
import { acceleratorDeviceName, highestUtilization, publicAccelerators } from "./accelerator-public"
import { acceleratorPolicyWrite } from "./accelerator-policy"
import { acceleratorDefinition } from "./accelerator-history"
import { prefixedTranslator } from "@/context/locales"
import { testLocaleValue } from "@/test/catalogs"
const en = prefixedTranslator(testLocaleValue("en").t, "accelerator")
const zh = prefixedTranslator(testLocaleValue("zh-CN").t, "accelerator")
import { expandPanelSeries, panelMetricQuery } from "./workspace-query"
import { validateWorkspacePanel } from "./workspace-panel-editor"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
describe("GPU telemetry presentation contracts", () => {
 it("retains zero values and strips raw identity, errors and unsupported metrics", () => {
  const source = acceleratorInventory().devices[0], copied = publicAccelerators([{ ...source, metrics: Object.assign({}, source.metrics, { gpu_power_watts: Infinity, private_metric: 1 }) }])![0]
  expect(copied).not.toHaveProperty("fingerprint"); expect(copied).not.toHaveProperty("driver_version"); expect(copied).not.toHaveProperty("error_codes"); expect(copied.metrics).not.toHaveProperty("private_metric"); expect(copied.metrics).not.toHaveProperty("gpu_power_watts")
  expect(publicAccelerators([{ ...source, metrics: { gpu_utilization_percent: 0 } }])![0].metrics.gpu_utilization_percent).toBe(0)
  expect(publicAccelerators(undefined)).toBeUndefined(); expect(publicAccelerators(Array(33).fill(source))).toEqual([])
  expect(publicAccelerators([{ ...source, device_id: "serial-number" }])).toEqual([])
  expect(publicAccelerators([{ ...source, metrics: { gpu_memory_used_bytes: 20, gpu_memory_total_bytes: 10 } }])![0].metrics).toEqual({})
 })
 it("uses stable aliases while models and ordinals remain display-only", () => {
  const first = acceleratorDevice(), second = { ...acceleratorDevice("b".repeat(32)), ordinal: 2, model: "AMD Device", metrics: { gpu_utilization_percent: 80 } }
  expect(acceleratorDeviceName(second)).toBe("GPU 2 · AMD Device"); expect(highestUtilization([first, second])?.device_id).toBe(second.device_id); expect(highestUtilization([{ ...first, metrics: {} }])).toBeUndefined()
  expect(acceleratorSeriesError({ metric: "gpu_utilization_percent", node_id: "$node", device_id: first.device_id })).toBe("workspace.gpu_fixed_node")
  expect(acceleratorSeriesError({ metric: "gpu_utilization_percent", node_id: "node-one" })).toBe("workspace.select_gpu_device")
  expect(acceleratorSeriesError({ metric: "cpu", node_id: "node-one", device_id: first.device_id })).toBe("workspace.invalid_panel")
 })
 it("enforces policy bounds and preserves explicit disablement and CAS", () => {
  const value = acceleratorPolicyWrite({ ...acceleratorPolicy(), configured: true, effective_source: "node", enabled: false, revision: 7 })
  expect(value).toMatchObject({ enabled: false, expected_revision: 7 }); expect(value).not.toHaveProperty("configured")
  for (const patch of [{ sample_interval_seconds: 4 }, { sample_interval_seconds: 3601 }, { maximum_devices: 0 }, { maximum_devices: 33 }, { metrics: ["cpu"] }, { metrics: [acceleratorMetricKeys[0], acceleratorMetricKeys[0]] }]) expect(() => acceleratorPolicyWrite({ ...acceleratorPolicy(), ...patch })).toThrow()
 })
 it("builds bounded per-device panels with compatible catalog units and exact selectors", () => {
  const device = acceleratorDevice(), catalog = acceleratorCatalog(), definition = acceleratorDefinition({ nodeID: "node-one", device, catalog, t: en })
  expect(definition.panels).toHaveLength(4); expect(definition.panels[1].series).toHaveLength(2)
  for (const panel of definition.panels) {
   expect(validateWorkspacePanel(panel, catalog)).toBeNull()
   const expanded = expandPanelSeries(panel, { definition, selections: {}, nodes: [workspaceNode()] }), query = panelMetricQuery(expanded, { from: "2026-10-07T00:00:00Z", to: "2026-10-07T01:00:00Z" })
   expect(query.max_points).toBe(600); expect(query.series.every(series => series.device_id === device.device_id)).toBe(true)
  }
  expect(acceleratorDefinition({ nodeID: "node-one", catalog, t: zh }).panels).toEqual([])
  expect(acceleratorDefinition({ nodeID: "node-one", device, catalog: catalog.map(metric => ({ ...metric, public: false })), t: zh }).panels).toEqual([])
  const malformed = definition.panels[0]; malformed.series[0].device_id = undefined; expect(validateWorkspacePanel(malformed, catalog)).toBe("workspace.select_gpu_device"); expect(() => expandPanelSeries(malformed, { definition, selections: {}, nodes: [workspaceNode()] })).toThrow("workspace.select_gpu_device")
 })
 it("formats only finite normalized values and bilingual labels", () => {
  for (const key of acceleratorMetricKeys) { expect(acceleratorMetricLabel(key, en)).not.toBe(key); expect(acceleratorMetricLabel(key, zh)).not.toBe(key); expect(validAcceleratorValue(key, Number.NaN)).toBe(false) }
  expect(acceleratorMetricLabel("unknown", en)).toBe("unknown"); expect(validAcceleratorValue("gpu_utilization_percent", 101)).toBe(false); expect(validAcceleratorValue("gpu_temperature_celsius", -101)).toBe(false); expect(validAcceleratorValue("gpu_power_watts", -1)).toBe(false)
 })
})

describe("GPU native stream identity", () => {
 it("merges only known devices and never refreshes samples from host metadata", async () => {
  const { mergeAcceleratorSamples } = await import("./accelerator-public")
  const device = acceleratorDevice()
  expect(mergeAcceleratorSamples(undefined, [device])).toBeUndefined()
  expect(mergeAcceleratorSamples([device], undefined)).toEqual([device])
  const result = mergeAcceleratorSamples([device], [{ ...device, model: "Injected model", captured_at: "2026-10-07T01:00:00Z", metrics: { gpu_utilization_percent: 50 } }, acceleratorDevice("b".repeat(32))])!
  expect(result).toHaveLength(1); expect(result[0].model).toBe(device.model); expect(result[0].metrics.gpu_utilization_percent).toBe(50)
  expect(mergeAcceleratorSamples([device], [{ ...device, captured_at: "2020-01-01T00:00:00Z", metrics: { gpu_utilization_percent: 99 } }])![0].metrics.gpu_utilization_percent).toBe(25)
  expect(mergeAcceleratorSamples([device], [])![0].stale).toBe(true)
 })
})
it("ages GPU samples from their native cadence, with a thirty-second minimum", async () => {
 const { acceleratorStale } = await import("./accelerator-public"), device = { ...acceleratorDevice(), cadence_ms: 5000 }
 const captured = Date.parse(device.captured_at!)
 expect(acceleratorStale(device, captured + 30000)).toBe(false); expect(acceleratorStale(device, captured + 30001)).toBe(true)
 expect(acceleratorStale({ ...device, cadence_ms: 60000 }, captured + 180000)).toBe(false); expect(acceleratorStale({ ...device, cadence_ms: 60000 }, captured + 180001)).toBe(true)
 expect(acceleratorStale({ ...device, captured_at: undefined }, captured)).toBe(true)
})
