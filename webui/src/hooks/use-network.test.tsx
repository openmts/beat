import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { useAdminNetworkTasks, useNetworkHistory, useNodeNetworkSummaries, usePublicNetworkQuality, usePublicNodeNetwork } from "@/hooks/use-network"
import * as networkApi from "@/lib/network-api"
import type { NetworkTaskView } from "@/types"

vi.mock("@/lib/network-api")

const view = { task: { id: "task" }, nodes: [] }
const history = {
  task_id: "task",
  node_id: "node",
  from: "2026-07-29T11:00:00.000Z",
  to: "2026-07-29T12:00:00.000Z",
  series: [],
}

describe("network hooks", () => {
  beforeEach(() => vi.resetAllMocks())

  it("refreshes public quality silently without replacing visible data", async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2026-07-29T12:00:00Z"))
      let resolveRefresh: ((value: never[]) => void) | undefined
      vi.mocked(networkApi.listPublicNetworkQuality)
        .mockResolvedValueOnce([view] as never)
        .mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve }))
        .mockRejectedValueOnce(new Error("background failed"))
      const rendered = renderHook(() => usePublicNetworkQuality())
      await act(async () => { await Promise.resolve() })
      expect(rendered.result.current.data).toEqual([view])

      await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
      expect(rendered.result.current.loading).toBe(false)
      expect(rendered.result.current.data).toEqual([view])
      act(() => resolveRefresh?.([{ ...view, task: { id: "new" } }] as never))
      await act(async () => { await Promise.resolve() })
      expect(rendered.result.current.data?.[0].task.id).toBe("new")

      await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
      expect(rendered.result.current.error).toBeNull()
      rendered.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it("loads admin tasks and reports foreground errors", async () => {
    vi.mocked(networkApi.listNetworkTasks).mockResolvedValueOnce([]).mockRejectedValueOnce("bad")
    const rendered = renderHook(() => useAdminNetworkTasks())
    await waitFor(() => expect(rendered.result.current.loading).toBe(false))
    await act(() => rendered.result.current.refresh())
    expect(rendered.result.current.error).toBe("Unknown error")
  })

  it("requests public and admin history with a stable selected range", async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2026-07-29T12:00:00Z"))
      vi.mocked(networkApi.getPublicNetworkHistory).mockResolvedValue(history)
      vi.mocked(networkApi.getAdminNetworkHistory).mockResolvedValue(history)
      const rendered = renderHook(
        ({ hours, admin }) => useNetworkHistory({
          taskId: "task",
          nodeId: "node",
          rangeHours: hours,
          admin,
          enabled: true,
        }),
        { initialProps: { hours: 1, admin: false } },
      )
      await act(async () => { await Promise.resolve() })
      expect(networkApi.getPublicNetworkHistory).toHaveBeenCalledWith("task", {
        nodeId: "node",
        from: "2026-07-29T11:00:00.000Z",
        to: "2026-07-29T12:00:00.000Z",
      })
      expect(rendered.result.current.from).toBe(1785322800)
      expect(rendered.result.current.to).toBe(1785326400)

      rendered.rerender({ hours: 6, admin: true })
      await act(async () => { await Promise.resolve() })
      expect(networkApi.getAdminNetworkHistory).toHaveBeenCalledWith("task", {
        nodeId: "node",
        from: "2026-07-29T06:00:00.000Z",
        to: "2026-07-29T12:00:00.000Z",
      })
      await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
      expect(rendered.result.current.loading).toBe(false)
      rendered.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it("skips disabled history and reports request failures", async () => {
    const disabled = renderHook(() => useNetworkHistory({ rangeHours: 1, enabled: false }))
    expect(disabled.result.current.loading).toBe(false)
    expect(networkApi.getPublicNetworkHistory).not.toHaveBeenCalled()
    disabled.unmount()

    vi.mocked(networkApi.getPublicNetworkHistory).mockRejectedValueOnce(new Error("history failed"))
    const failed = renderHook(() => useNetworkHistory({
      taskId: "task",
      nodeId: "node",
      rangeHours: 1,
      enabled: true,
    }))
    await waitFor(() => expect(failed.result.current.error).toBe("history failed"))
  })

  it("aggregates node summaries and avoids requests when disabled", async () => {
    vi.mocked(networkApi.listPublicNetworkQuality).mockResolvedValueOnce([qualityView] as never)
    const enabled = renderHook(() => useNodeNetworkSummaries(true))
    await waitFor(() => expect(enabled.result.current.loading).toBe(false))
    expect(enabled.result.current.summaries["node-one"]).toMatchObject({
      type: "icmp", direction: "server_to_agent", latency_ms: 12, loss_percent: 5,
    })
    enabled.unmount()

    vi.mocked(networkApi.listPublicNetworkQuality).mockClear()
    const disabled = renderHook(() => useNodeNetworkSummaries(false))
    await act(async () => { await Promise.resolve() })
    expect(disabled.result.current.loading).toBe(false)
    expect(networkApi.listPublicNetworkQuality).not.toHaveBeenCalled()
  })

  it("loads public node history for the selected range and preserves failed samples as gaps", async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2026-07-29T12:00:00Z"))
      const privateView = { ...qualityView, task: { ...qualityView.task, id: "private", is_public: false } }
      const unassignedView = { ...qualityView, task: { ...qualityView.task, id: "other" }, nodes: [] }
      vi.mocked(networkApi.listPublicNetworkQuality).mockResolvedValue([qualityView, privateView, unassignedView])
      vi.mocked(networkApi.getPublicNetworkHistory).mockResolvedValue({
        ...history,
        series: [{
          target_id: "target-id", label: "移动", carrier: "mobile", region: "", visible_address: false,
          points: [
            { timestamp: "2026-07-29T11:59:00Z", average_latency_ms: 12.5, success_percent: 100, sample_count: 1 },
            { timestamp: "2026-07-29T11:59:30Z", average_latency_ms: 0, success_percent: 0, sample_count: 1 },
          ],
        }],
      })
      const rendered = renderHook(() => usePublicNodeNetwork("node-one", { mode: "relative", seconds: 3600 }))
      await flushMicrotasks()
      expect(rendered.result.current.history.data).not.toBeNull()
      expect(networkApi.getPublicNetworkHistory).toHaveBeenCalledWith("task", expect.objectContaining({
        nodeId: "node-one", from: "2026-07-29T11:00:00.000Z", to: "2026-07-29T12:00:00.000Z", signal: expect.any(AbortSignal),
      }))
      expect(rendered.result.current.history.data?.tasks).toEqual([{
        task_id: "task",
        points: [
          { timestamp: Date.parse("2026-07-29T11:59:00Z") / 1000, latency_ms: 12.5 },
          { timestamp: Date.parse("2026-07-29T11:59:30Z") / 1000, latency_ms: null },
        ],
        series: [{
          target_id: "target-id", label: "移动", carrier: "mobile", region: "", visible_address: false,
          points: [
            { timestamp: Date.parse("2026-07-29T11:59:00Z") / 1000, latency_ms: 12.5 },
            { timestamp: Date.parse("2026-07-29T11:59:30Z") / 1000, latency_ms: null },
          ],
        }],
      }])
      rendered.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it("clears node history when the selected range changes and skips disabled network data", async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2026-07-29T12:00:00Z"))
      vi.mocked(networkApi.listPublicNetworkQuality).mockResolvedValue([qualityView])
      vi.mocked(networkApi.getPublicNetworkHistory).mockResolvedValue(history)
      const rendered = renderHook(({ seconds }) => usePublicNodeNetwork("node-one", { mode: "relative", seconds }), { initialProps: { seconds: 3600 } })
      await flushMicrotasks()
      expect(rendered.result.current.history.loading).toBe(false)
      rendered.rerender({ seconds: 21600 })
      await flushMicrotasks()
      expect(networkApi.getPublicNetworkHistory).toHaveBeenLastCalledWith("task", expect.objectContaining({ from: "2026-07-29T06:00:00.000Z" }))
      rendered.unmount()

      vi.mocked(networkApi.listPublicNetworkQuality).mockClear()
      const disabled = renderHook(() => usePublicNodeNetwork("node-one", { mode: "relative", seconds: 3600 }, false))
      await flushMicrotasks()
      expect(disabled.result.current.history.data).toBeNull()
      expect(networkApi.listPublicNetworkQuality).not.toHaveBeenCalled()
      disabled.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})

async function flushMicrotasks() {
  await act(async () => {
    for (let index = 0; index < 6; index += 1) await Promise.resolve()
  })
}

const qualityView: NetworkTaskView = {
  task: {
    id: "task", name: "Server probe", direction: "server_to_agent", type: "icmp", target_mode: "single",
    target: "{{host}}", targets: [],
    ip_family: "auto", interval_seconds: 60, timeout_milliseconds: 3000, packet_count: 5,
    all_nodes: true, enabled: true, is_public: true, sort_order: 0, nodes: [], created_at: "", updated_at: "",
  },
  nodes: [{
    node: { id: "node-one", name: "node-one", alias: "" },
    latest: { timestamp: "2026-07-29T00:00:00Z", latency_ms: 12, success: true, status_code: 0,
      error_code: "none", loss_percent: 5 },
  }],
}
