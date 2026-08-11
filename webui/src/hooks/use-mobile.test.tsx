import { act, renderHook } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { useIsMobile } from "@/hooks/use-mobile"

it("tracks the mobile breakpoint and removes its listener", () => {
  let change: (() => void) | undefined
  const remove = vi.fn()
  vi.spyOn(window, "matchMedia").mockReturnValue({
    addEventListener: (_: string, listener: () => void) => { change = listener },
    removeEventListener: remove,
  } as unknown as MediaQueryList)
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: 500 })
  const { result, unmount } = renderHook(() => useIsMobile())
  expect(result.current).toBe(true)
  act(() => {
    window.innerWidth = 900
    change?.()
  })
  expect(result.current).toBe(false)
  unmount()
  expect(remove).toHaveBeenCalled()
})
