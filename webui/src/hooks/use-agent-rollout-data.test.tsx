import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useAgentRolloutData } from "./use-agent-rollout-data"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner" }, session: { id: "session" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  const value = testLocaleValue("en")
  return { useLocale: () => value, useOptionalLocale: () => value }
})
afterEach(() => { vi.useRealTimers(); auth.principal.session.id = "session" })
describe("Agent rollout background reads", () => {
 it("polls visible pages and aborts stale refreshes, identity changes and unmounts", async () => {
  vi.useFakeTimers()
  let finish!: (value: string) => void
  const fetcher = vi.fn<(signal: AbortSignal) => Promise<string>>().mockResolvedValue("current")
  const view = renderHook(() => useAgentRolloutData(fetcher)); await act(async () => {})
  expect(view.result.current.data).toBe("current")
  fetcher.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
  const pending = fetcher.mock.calls[1][0]
  await act(async () => { await view.result.current.refresh() }); expect(pending.aborted).toBe(true)
  await act(async () => finish("stale")); expect(view.result.current.data).toBe("current")
  const active = fetcher.mock.calls.at(-1)![0]; auth.principal.session.id = "different"; view.rerender(); await act(async () => {})
  expect(active.aborted).toBe(true)
  const final = fetcher.mock.calls.at(-1)![0]; view.unmount(); expect(final.aborted).toBe(true)
 })
 it("reports refresh errors and clears previous privileged data", async () => {
  const fetcher = vi.fn<(signal: AbortSignal) => Promise<string>>().mockResolvedValue("private")
  const view = renderHook(() => useAgentRolloutData(fetcher)); await act(async () => {})
  fetcher.mockRejectedValueOnce(new Error("permission expired")); await act(async () => { await view.result.current.refresh() })
  expect(view.result.current.data).toBeNull(); expect(view.result.current.error).toBe("permission expired")
 })
})
