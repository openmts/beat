import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useActiveTheme } from "@/hooks/use-active-theme"

const fetchTheme = vi.hoisted(() => vi.fn())
vi.mock("@/lib/theme-api", () => ({ getActiveTheme: fetchTheme }))
beforeEach(() => { fetchTheme.mockReset() })
describe("public active theme loading", () => {
  it("loads the active descriptor", async () => {
    const descriptor = { theme_id: "theme", protocol: 2 }
    fetchTheme.mockResolvedValue(descriptor)
    const hook = renderHook(useActiveTheme)
    await waitFor(() => expect(hook.result.current).toEqual(descriptor))
  })
  it("falls back when the public descriptor request fails", async () => {
    fetchTheme.mockRejectedValue(new Error("unavailable"))
    const hook = renderHook(useActiveTheme)
    await act(async () => { await Promise.resolve() })
    expect(hook.result.current).toBeNull()
  })
  it("ignores responses after leaving the page", async () => {
    let resolve!: (value: unknown) => void
    fetchTheme.mockReturnValue(new Promise((done) => { resolve = done }))
    const hook = renderHook(useActiveTheme); hook.unmount()
    await act(async () => { resolve({ theme_id: "stale" }); await Promise.resolve() })
    expect(hook.result.current).toBeNull()
  })
})
