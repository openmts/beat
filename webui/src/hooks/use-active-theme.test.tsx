import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { cachedActiveTheme, preloadActiveTheme, resetActiveThemeCache, useActiveTheme } from "@/hooks/use-active-theme"

const fetchTheme = vi.hoisted(() => vi.fn())
vi.mock("@/lib/theme-api", () => ({ getActiveTheme: fetchTheme }))
beforeEach(() => { fetchTheme.mockReset(); resetActiveThemeCache() })
describe("public active theme loading", () => {
  it("loads the active descriptor", async () => {
    const descriptor = { theme_id: "theme", protocol: 3 }
    fetchTheme.mockResolvedValue(descriptor)
    const hook = renderHook(useActiveTheme)
    await waitFor(() => expect(hook.result.current).toEqual(descriptor))
  })
  it("returns a preloaded descriptor synchronously without a second request", async () => {
    const descriptor = { theme_id: "theme", protocol: 3 }
    fetchTheme.mockResolvedValue(descriptor)
    await act(async () => { await preloadActiveTheme() })
    expect(cachedActiveTheme()).toEqual({ status: "ready", descriptor })
    const hook = renderHook(useActiveTheme)
    expect(hook.result.current).toEqual(descriptor)
    expect(fetchTheme).toHaveBeenCalledOnce()
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
