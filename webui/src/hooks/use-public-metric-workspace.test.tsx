import { act, render, renderHook, screen, waitFor } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { usePublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"
import * as publicAPI from "@/lib/public-workspace-api"
import { queryWorkspaceMetrics } from "@/lib/metric-workspace-api"
import { workspaceDashboard, workspaceNode, workspaceResult, workspaceSources } from "@/test/metric-workspace-fixtures"
import { APIError } from "@/lib/api-error"
import NodeDetail from "@/pages/node-detail"
import type { ThemeSnapshotInput } from "@/types/theme-runtime"

vi.mock("@/lib/public-workspace-api")
vi.mock("@/lib/metric-workspace-api", () => ({ queryWorkspaceMetrics: vi.fn() }))
const boundary = vi.hoisted(() => ({ input: null as unknown }))
vi.mock("@/components/public-theme-boundary", () => ({ PublicThemeBoundary: (props: ThemeSnapshotInput) => { boundary.input = props; return <p>Script theme is active</p> } }))
const sources = () => ({ ...workspaceSources(), dashboard: { ...workspaceDashboard(), scope: "public" as const, definition: { ...workspaceDashboard().definition, refresh_seconds: 5 } } })

describe("public node metric workspace lifecycle", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(sources())
    vi.mocked(publicAPI.getPublicNodeSummary).mockResolvedValue(workspaceNode())
    vi.mocked(queryWorkspaceMetrics).mockResolvedValue(workspaceResult())
  })
  afterEach(() => { vi.useRealTimers() })
  it("loads a node template and exposes successful history using the public query path", async () => {
    const { result, unmount } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await waitFor(() => expect(result.current.history?.series.cpu).toHaveLength(2))
    expect(result.current.values.node).toEqual(["node-one"])
    expect(result.current.node.data?.id).toBe("node-one")
    expect(queryWorkspaceMetrics).toHaveBeenCalledWith(expect.objectContaining({ series: [expect.objectContaining({ node_id: "node-one" })] }), expect.objectContaining({ public: true }))
    expect(publicAPI.getPublicWorkspaceSources).toHaveBeenCalledOnce()
    unmount()
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls[0][0].signal.aborted).toBe(true)
  })
  it("refreshes summary and queries with a bounded cadence, pauses both, and manually refreshes", async () => {
    vi.useFakeTimers()
    const { result, unmount } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const initial = vi.mocked(publicAPI.getPublicNodeSummary).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(publicAPI.getPublicNodeSummary).toHaveBeenCalledTimes(initial + 1)
    act(() => result.current.setRefreshSeconds(0))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const paused = vi.mocked(publicAPI.getPublicNodeSummary).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(publicAPI.getPublicNodeSummary).toHaveBeenCalledTimes(paused)
    act(() => result.current.refresh())
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(publicAPI.getPublicNodeSummary).toHaveBeenCalledTimes(paused + 1)
    unmount()
  })
  it("aborts pending requests on route changes and ignores late responses", async () => {
    let finish: ((value: ReturnType<typeof sources>) => void) | undefined
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const { result, rerender, unmount } = renderHook(({ id }) => usePublicMetricWorkspace(id), { initialProps: { id: "node-one" } })
    const pending = vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls[0][0].signal
    vi.mocked(publicAPI.getPublicNodeSummary).mockResolvedValue(workspaceNode("node-two"))
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue({ ...sources(), nodes: [workspaceNode("node-two")] })
    rerender({ id: "node-two" })
    expect(pending.aborted).toBe(true)
    await waitFor(() => expect(result.current.node.data?.id).toBe("node-two"))
    await act(async () => { finish?.(sources()) })
    expect(result.current.values.node).toEqual(["node-two"])
    unmount()
  })
  it("clears a node that becomes private instead of forwarding retained private history", async () => {
    const { result } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await waitFor(() => expect(result.current.history).toBeDefined())
    vi.mocked(publicAPI.getPublicNodeSummary).mockRejectedValue(new APIError("hidden", 404))
    act(() => result.current.refresh())
    await waitFor(() => expect(result.current.node.data).toBeNull())
    expect(result.current.history).toBeUndefined()
    expect(result.current.node.error).toBe("workspace.node_unavailable")
  })
  it("queries outside the theme boundary even when the script theme suppresses the built-in view", async () => {
    render(<MemoryRouter initialEntries={["/node/node-one"]}><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></MemoryRouter>)
    expect(screen.getByText("Script theme is active")).toBeInTheDocument()
    await waitFor(() => expect((boundary.input as ThemeSnapshotInput).history?.series?.cpu[1].value).toBeNull())
    expect((boundary.input as ThemeSnapshotInput).history?.series_info?.cpu).toEqual({ aggregation: "avg", unit: "percent" })
    expect(queryWorkspaceMetrics).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ public: true }))
  })
  it("retries a transient source failure until the server recovers", async () => {
    vi.useFakeTimers()
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValueOnce(new APIError("bad gateway", 502))
      .mockRejectedValueOnce(new APIError("bad gateway", 502)).mockResolvedValue(sources())
    const { result, unmount } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    await act(async () => { await vi.advanceTimersByTimeAsync(500) })
    await act(async () => { await vi.advanceTimersByTimeAsync(1_200) })
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls.map(([options]) => options.attempt)).toEqual([0, 1, 2])
    expect(result.current.source.error).toBeNull()
    expect(result.current.source.data).not.toBeNull()
    unmount()
  })
  it("reports a deterministic source failure and recovers on manual reload", async () => {
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValue(new APIError("gone", 404))
    const { result } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await waitFor(() => expect(result.current.source.error).toBe("workspace.load_failed"))
    expect(result.current.metricsState).toBe("error")
    expect(result.current.history).toBeUndefined()
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls.map(([options]) => options.attempt)).toEqual([0])
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(sources())
    act(() => result.current.source.reload())
    await waitFor(() => expect(result.current.history?.series.cpu).toBeDefined())
    expect(result.current.source.error).toBeNull()
  })
  it("retries the workspace when the tab becomes visible again after an outage", async () => {
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValue(new APIError("gone", 404))
    const { result } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await waitFor(() => expect(result.current.source.error).toBe("workspace.load_failed"))
    const attempts = vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls.length
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(sources())
    act(() => { document.dispatchEvent(new Event("visibilitychange")) })
    await waitFor(() => expect(result.current.source.error).toBeNull())
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls.length).toBeGreaterThan(attempts)
  })
  it("supports group selections and explicit public comparison nodes without exporting another node's history", async () => {
    const source = sources()
    source.nodes.push({ ...workspaceNode("node-two"), group_id: "group-two" })
    source.groups.push({ ...source.groups[0], id: "group-two", name: "Other group" })
    source.dashboard.definition.variables.push({ name: "group", type: "group", label: "Group" })
    source.dashboard.definition.panels.push({ ...source.dashboard.definition.panels[0], id: "memory", series: [{ ...source.dashboard.definition.panels[0].series[0], node_id: "$group", metric: "memory" }] },
      { ...source.dashboard.definition.panels[0], id: "compare", series: [{ ...source.dashboard.definition.panels[0].series[0], node_id: "node-two" }] })
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(source)
    vi.mocked(queryWorkspaceMetrics).mockImplementation(async (query) => ({ from: query.from, to: query.to, interval_ms: 1000,
      series: query.series.map((series) => ({ ...series, label: series.metric, unit: "percent", available_from: null, state: "ok" as const, points: [{ timestamp: Date.parse(query.from), value: 10 }] })) }))
    const { result } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await waitFor(() => expect(result.current.history?.series.memory).toBeDefined())
    expect(result.current.values.group).toEqual(["group-one"])
    expect(vi.mocked(queryWorkspaceMetrics).mock.calls.some(([query]) => query.series.some((series) => series.node_id === "node-two"))).toBe(true)
    act(() => result.current.setVariable("group", ["group-two"]))
    await waitFor(() => expect(result.current.queries.panels.memory?.result?.series[0].node_id).toBe("node-two"))
    expect(result.current.history?.series).not.toHaveProperty("memory")
    act(() => result.current.setRange({ mode: "absolute", from: "2026-09-13T00:00:00Z", to: "2026-09-13T01:00:00Z" }))
    await waitFor(() => expect(result.current.queries.domain.from).toBe("2026-09-13T00:00:00.000Z"))
  })
  it("keeps only two slow panel queries active while summary refreshes independently", async () => {
    vi.useFakeTimers()
    const source = sources()
    source.dashboard.definition.panels = Array.from({ length: 4 }, (_, index) => ({ ...source.dashboard.definition.panels[0], id: `panel-${index}` }))
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(source)
    const replies: (() => void)[] = []
    let active = 0, maximum = 0
    vi.mocked(queryWorkspaceMetrics).mockImplementation((query) => new Promise((resolve) => {
      maximum = Math.max(maximum, ++active)
      replies.push(() => { active--; resolve({ ...workspaceResult(), from: query.from, to: query.to }) })
    }))
    const { unmount } = renderHook(() => usePublicMetricWorkspace("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
    const summaries = vi.mocked(publicAPI.getPublicNodeSummary).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    expect(publicAPI.getPublicNodeSummary).toHaveBeenCalledTimes(summaries + 2)
    expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
    await act(async () => { replies[0](); replies[1](); await vi.advanceTimersByTimeAsync(0) })
    expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(4)
    await act(async () => { replies[2](); replies[3](); await vi.advanceTimersByTimeAsync(0) })
    expect(maximum).toBe(2)
    unmount()
  })
})
