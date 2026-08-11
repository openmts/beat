import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import * as api from "@/lib/api"
import {
  useAlertChannels, useAlertEvents, useAlertRules, useGroups,
  useLiveNodes, useManagedNodes, useNode, useNodeMetrics, useNodes, useSSHKeys,
  useTrafficReportSchedules,
} from "@/hooks/use-api"

vi.mock("@/lib/api")

describe("API hooks", () => {
  beforeEach(() => vi.resetAllMocks())

  it("loads and refreshes list hooks", async () => {
    vi.mocked(api.listNodes).mockResolvedValue([])
    const { result, rerender } = renderHook(({ group }) => useNodes(group), { initialProps: { group: "one" } })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(api.listNodes).toHaveBeenCalledWith("one")
    await act(() => result.current.refresh())
    rerender({ group: "two" })
    await waitFor(() => expect(api.listNodes).toHaveBeenCalledWith("two"))
  })

  it("applies live node snapshots and preserves group filtering", async () => {
    class MockWebSocket {
      static instances: MockWebSocket[] = []
      onmessage: ((event: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      onclose: (() => void) | null = null
      close = vi.fn()
      url: string
      constructor(url: string) {
        this.url = url
        MockWebSocket.instances.push(this)
      }
    }
    vi.stubGlobal("WebSocket", MockWebSocket)
    vi.mocked(api.listNodes).mockResolvedValue([{ id: "rest", group_id: "group-a" }] as never)
    const rendered = renderHook(() => useLiveNodes("group-a"))
    await waitFor(() => expect(rendered.result.current.data?.[0]?.id).toBe("rest"))
    expect(MockWebSocket.instances[0].url).toContain("/api/v1/ws/metrics")
    act(() => MockWebSocket.instances[0].onmessage?.({
      data: JSON.stringify({ nodes: [
        { id: "live-a", group_id: "group-a" },
        { id: "live-b", group_id: "group-b" },
      ] }),
    }))
    expect(rendered.result.current.data?.map((node) => node.id)).toEqual(["live-a"])
    act(() => MockWebSocket.instances[0].onmessage?.({ data: "invalid" }))
    expect(rendered.result.current.data?.[0]?.id).toBe("live-a")
    rendered.unmount()
    expect(MockWebSocket.instances[0].close).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it("keeps existing list data visible during a silent refresh", async () => {
    let resolveRefresh: ((value: never[]) => void) | undefined
    vi.mocked(api.listNodes)
      .mockResolvedValueOnce([{ id: "old" }] as never)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve }))
    const { result } = renderHook(() => useNodes())
    await waitFor(() => expect(result.current.data?.[0]?.id).toBe("old"))

    let refreshPromise: Promise<void> | undefined
    act(() => { refreshPromise = result.current.refresh({ silent: true }) })
    expect(result.current.loading).toBe(false)
    expect(result.current.data?.[0]?.id).toBe("old")
    act(() => resolveRefresh?.([{ id: "new" }] as never))
    await act(async () => refreshPromise)
    expect(result.current.data?.[0]?.id).toBe("new")
  })

  it("reports list errors including non-Error values", async () => {
    vi.mocked(api.listGroups).mockRejectedValueOnce(new Error("failed")).mockRejectedValueOnce("bad")
    const first = renderHook(() => useGroups())
    await waitFor(() => expect(first.result.current.error).toBe("failed"))
    first.unmount()
    const second = renderHook(() => useGroups())
    await waitFor(() => expect(second.result.current.error).toBe("Unknown error"))
  })

  it("loads node and handles missing IDs and errors", async () => {
    vi.mocked(api.getNode).mockResolvedValue({ id: "node" } as never)
    const loaded = renderHook(() => useNode("node"))
    await waitFor(() => expect(loaded.result.current.data?.id).toBe("node"))
    const missing = renderHook(() => useNode(undefined))
    await waitFor(() => expect(missing.result.current.error).toBe("Node ID is required"))
    vi.mocked(api.getNode).mockRejectedValueOnce("bad")
    const failed = renderHook(() => useNode("bad"))
    await waitFor(() => expect(failed.result.current.error).toBe("Unknown error"))
  })

  it("loads metrics for the selected range, refreshes silently, and handles errors", async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2026-07-29T12:00:00Z"))
      vi.mocked(api.getNodeMetrics).mockResolvedValue({ cpu: [] })
      const loaded = renderHook(({ hours }) => useNodeMetrics("node", ["cpu"], hours), {
        initialProps: { hours: 1 },
      })
      await act(async () => { await Promise.resolve() })
      expect(loaded.result.current.data).toEqual({ cpu: [] })
      expect(api.getNodeMetrics).toHaveBeenCalledWith(
        "node", ["cpu"], "2026-07-29T11:00:00.000Z", "2026-07-29T12:00:00.000Z",
      )

      loaded.rerender({ hours: 6 })
      await act(async () => { await Promise.resolve() })
      expect(api.getNodeMetrics).toHaveBeenCalledWith(
        "node", ["cpu"], "2026-07-29T06:00:00.000Z", "2026-07-29T12:00:00.000Z",
      )
      expect(loaded.result.current.loading).toBe(false)

      await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
      expect(api.getNodeMetrics).toHaveBeenLastCalledWith(
        "node", ["cpu"], "2026-07-29T06:00:10.000Z", "2026-07-29T12:00:10.000Z",
      )
      expect(loaded.result.current.loading).toBe(false)
      loaded.unmount()
    } finally {
      vi.useRealTimers()
    }

    const missing = renderHook(() => useNodeMetrics(undefined))
    expect(missing.result.current.loading).toBe(false)
    vi.mocked(api.getNodeMetrics).mockRejectedValueOnce(new Error("metrics failed"))
    const failed = renderHook(() => useNodeMetrics("bad"))
    await waitFor(() => expect(failed.result.current.error).toBe("metrics failed"))
  })

  it("loads all specialized list hooks", async () => {
    vi.mocked(api.listSSHKeys).mockResolvedValue([])
    vi.mocked(api.listAlertRules).mockResolvedValue([])
    vi.mocked(api.listAlertChannels).mockResolvedValue([])
    vi.mocked(api.listAlertEvents).mockResolvedValue([])
    vi.mocked(api.listManagedNodes).mockResolvedValue([])
    vi.mocked(api.listTrafficReportSchedules).mockResolvedValue([])
    const hooks = [
      useSSHKeys, useAlertRules, useAlertChannels, useAlertEvents,
      useManagedNodes, useTrafficReportSchedules,
    ]
    for (const hook of hooks) {
      const rendered = renderHook(() => hook())
      await waitFor(() => expect(rendered.result.current.loading).toBe(false))
      rendered.unmount()
    }
  })
})
