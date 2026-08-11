import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { useAdminNetworkTasks, useNetworkHistory, usePublicNetworkQuality } from "@/hooks/use-network"
import * as networkApi from "@/lib/network-api"

vi.mock("@/lib/network-api")

const view = { task: { id: "task" }, nodes: [] }
const history = {
  task_id: "task",
  node_id: "node",
  from: "2026-07-29T11:00:00.000Z",
  to: "2026-07-29T12:00:00.000Z",
  points: [],
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
})
