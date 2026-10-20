import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { usePublicWorkspaceSources } from "@/hooks/use-public-workspace-sources"
import * as publicAPI from "@/lib/public-workspace-api"
import { APIError } from "@/lib/api-error"
import { workspaceDashboard, workspaceMetric, workspaceNode } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/public-workspace-api")

const sources = () => ({ dashboard: { ...workspaceDashboard(), scope: "public" as const },
  catalog: { version: 1, revision: 1, metrics: [workspaceMetric()] }, nodes: [workspaceNode()], groups: [] })

// 首屏退避预算：400+1000+2500+5000+8000+12000ms 之后落到错误态，再等一次慢速恢复。
const fastBudgetMs = 35_000

describe("public workspace source recovery", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.useFakeTimers()
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
  })
  afterEach(() => { vi.useRealTimers() })

  it("recovers without a manual reload once the service returns", async () => {
    for (let index = 0; index < 7; index += 1) vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValueOnce(new APIError("bad gateway", 502))
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(sources())
    const { result, unmount } = renderHook(() => usePublicWorkspaceSources("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(fastBudgetMs) })
    expect(result.current.error).toBe("workspace.load_failed")
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls).toHaveLength(7)
    await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
    expect(result.current.error).toBeNull()
    expect(result.current.data).not.toBeNull()
    unmount()
  })

  it("keeps a one-per-minute cadence once the slow budget is spent so long upgrades self-heal", async () => {
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValue(new APIError("down", 503))
    const { result, unmount } = renderHook(() => usePublicWorkspaceSources("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(10 * 60_000) })
    expect(result.current.error).toBe("workspace.load_failed")
    // 6 次首屏退避 + 15 次 20 s 慢速恢复，之后每分钟一次驻留重试（10 min 内再补 4 次）。
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls).toHaveLength(26)
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockResolvedValue(sources())
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(result.current.error).toBeNull()
    expect(result.current.data).not.toBeNull()
    unmount()
  })

  it("does not slow-retry deterministic failures such as a missing node", async () => {
    vi.mocked(publicAPI.getPublicWorkspaceSources).mockRejectedValue(new APIError("gone", 404))
    const { result, unmount } = renderHook(() => usePublicWorkspaceSources("node-one"))
    await act(async () => { await vi.advanceTimersByTimeAsync(10 * 60_000) })
    expect(result.current.error).toBe("workspace.load_failed")
    expect(vi.mocked(publicAPI.getPublicWorkspaceSources).mock.calls).toHaveLength(1)
    unmount()
  })
})
