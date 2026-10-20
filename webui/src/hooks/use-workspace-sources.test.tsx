import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useWorkspaceSources } from "@/hooks/use-workspace-sources"
import * as workspaceAPI from "@/lib/metric-workspace-api"
import { APIError } from "@/lib/api-error"
import { workspaceSources } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/metric-workspace-api")

const fastBudgetMs = 35_000

describe("admin workspace source recovery", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.useFakeTimers()
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
  })
  afterEach(() => { vi.useRealTimers() })

  it("clears the error state on its own after an upgrade window", async () => {
    for (let index = 0; index < 7; index += 1) vi.mocked(workspaceAPI.getWorkspaceSources).mockRejectedValueOnce(new APIError("unavailable", 503))
    vi.mocked(workspaceAPI.getWorkspaceSources).mockResolvedValue(workspaceSources())
    const { result, unmount } = renderHook(() => useWorkspaceSources())
    await act(async () => { await vi.advanceTimersByTimeAsync(fastBudgetMs) })
    expect(result.current.error).toBe("unavailable")
    expect(vi.mocked(workspaceAPI.getWorkspaceSources).mock.calls).toHaveLength(7)
    await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
    expect(result.current.error).toBeNull()
    expect(result.current.sources).not.toBeNull()
    unmount()
  })

  it("keeps a one-per-minute cadence after the slow recovery budget is spent", async () => {
    vi.mocked(workspaceAPI.getWorkspaceSources).mockRejectedValue(new APIError("down", 502))
    const { result, unmount } = renderHook(() => useWorkspaceSources())
    await act(async () => { await vi.advanceTimersByTimeAsync(10 * 60_000) })
    expect(result.current.error).toBe("down")
    expect(vi.mocked(workspaceAPI.getWorkspaceSources).mock.calls).toHaveLength(26)
    await act(async () => { await vi.advanceTimersByTimeAsync(5 * 60_000) })
    expect(vi.mocked(workspaceAPI.getWorkspaceSources).mock.calls).toHaveLength(31)
    unmount()
  })
})
