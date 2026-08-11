import { describe, expect, it, vi } from "vitest"
import { emitAuthInvalidated, subscribeAuthInvalidated } from "@/lib/auth"

describe("admin session events", () => {
  it("notifies and unsubscribes invalidation listeners", () => {
    const listener = vi.fn()
    const unsubscribe = subscribeAuthInvalidated(listener)
    emitAuthInvalidated()
    unsubscribe()
    emitAuthInvalidated()
    expect(listener).toHaveBeenCalledTimes(1)
  })

  it("does not store administrator credentials in web storage", () => {
    emitAuthInvalidated()
    expect(sessionStorage.length).toBe(0)
    expect(localStorage.getItem("beat.admin.token")).toBeNull()
  })
})
