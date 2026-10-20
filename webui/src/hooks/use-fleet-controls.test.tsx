import { act, renderHook } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { useFleetControls } from "./use-fleet-controls"
import type { Group } from "@/types"
afterEach(() => vi.restoreAllMocks())
describe("URL-backed fleet controls", () => {
 it("drops unknown filter values and stores layout only", () => {
  const wrapper = ({ children }: { children: React.ReactNode }) => <MemoryRouter initialEntries={["/?group=known&group=unknown&tag=edge&tag=private"]}>{children}</MemoryRouter>
  const hook = renderHook(() => useFleetControls([{ ...workspaceNode(), tags: ["edge"] }], [{ id: "known" }] as Group[]), { wrapper })
  expect(hook.result.current.value.groups).toEqual(["known"]); expect(hook.result.current.value.tags).toEqual(["edge"])
  act(() => hook.result.current.change({ layout: "table", search: "hello" })); expect(localStorage.getItem("beat.fleet.layout")).toBe("table"); expect(hook.result.current.value.search).toBe("hello")
  expect(localStorage.length).toBe(1)
 })
 it("uses desktop layout preference and tolerates blocked browser storage", () => {
  localStorage.setItem("beat.fleet.layout", "table")
  const wrapper = ({ children }: { children: React.ReactNode }) => <MemoryRouter>{children}</MemoryRouter>
  const hook = renderHook(() => useFleetControls([], []), { wrapper }); expect(hook.result.current.value.layout).toBe("table")
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("disabled") }); act(() => hook.result.current.change({ layout: "cards" })); expect(hook.result.current.value.layout).toBe("cards")
  hook.unmount(); vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("disabled") }); const fallback = renderHook(() => useFleetControls([], []), { wrapper }); expect(fallback.result.current.value.layout).toBe("cards")
 })
})
