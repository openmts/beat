import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { defaultFleetControls } from "@/lib/fleet-state"
import { useFleetOrder } from "./use-fleet-order"
import type { Group } from "@/types"
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })
describe("fleet presentation cadence", () => {
 it("samples metric sort every five seconds and freezes focused targets", () => {
  vi.useFakeTimers()
  const nodes = [{ ...workspaceNode("a"), telemetry_state: "fresh" as const, metrics: { cpu: 10 } }, { ...workspaceNode("b"), telemetry_state: "fresh" as const, metrics: { cpu: 20 } }]
  const groups = [{ id: "group-one", sort_order: 0 }] as Group[], controls = { ...defaultFleetControls, sort: "cpu" as const }
  const hook = renderHook(({ values, paused }) => useFleetOrder(values, groups, controls, "en", paused), { initialProps: { values: nodes, paused: false } })
  expect(hook.result.current.map(node => node.id)).toEqual(["a", "b"])
  hook.rerender({ values: [{ ...nodes[0], metrics: { cpu: 30 } }, nodes[1]], paused: false })
  expect(hook.result.current.map(node => node.id)).toEqual(["a", "b"])
  act(() => vi.advanceTimersByTime(5000)); expect(hook.result.current.map(node => node.id)).toEqual(["b", "a"])
  hook.rerender({ values: nodes, paused: true }); act(() => vi.advanceTimersByTime(10000)); expect(hook.result.current.map(node => node.id)).toEqual(["b", "a"])
  hook.rerender({ values: nodes, paused: false }); expect(hook.result.current.map(node => node.id)).toEqual(["a", "b"])
 })
})
