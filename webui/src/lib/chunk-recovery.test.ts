import { describe, expect, it, vi } from "vitest"
import { installChunkRecovery, recoverFromChunkFailure, type ChunkStorage } from "./chunk-recovery"

type StubStorage = ChunkStorage & { value: () => string | null }

function memoryStorage(initial?: string): StubStorage {
  let stored = initial ?? null
  return { getItem: () => stored, setItem: (_key, value) => { stored = value }, value: () => stored }
}

describe("chunk recovery", () => {
  it("reloads once and records the attempt", () => {
    const storage = memoryStorage(), reload = vi.fn()
    expect(recoverFromChunkFailure({ storage, reload, now: () => 1_000_000 })).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    expect(storage.value()).toBe("1000000")
  })
  it("keeps the current page inside the guard window", () => {
    const storage = memoryStorage(), reload = vi.fn()
    expect(recoverFromChunkFailure({ storage, reload, now: () => 10_000_000 })).toBe(true)
    expect(recoverFromChunkFailure({ storage, reload, now: () => 10_029_999 })).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
  })
  it("recovers again after the window elapses", () => {
    const storage = memoryStorage(), reload = vi.fn()
    recoverFromChunkFailure({ storage, reload, now: () => 20_000_000 })
    expect(recoverFromChunkFailure({ storage, reload, now: () => 20_030_000 })).toBe(true)
    expect(reload).toHaveBeenCalledTimes(2)
  })
  it("falls back to the in-memory stamp without storage", () => {
    const reload = vi.fn()
    expect(recoverFromChunkFailure({ storage: null, reload, now: () => 30_000_000 })).toBe(true)
    expect(recoverFromChunkFailure({ storage: null, reload, now: () => 30_000_001 })).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
  })
  it("ignores an unreadable stamp", () => {
    const storage = memoryStorage("not-a-number"), reload = vi.fn()
    expect(recoverFromChunkFailure({ storage, reload, now: () => 40_000_000 })).toBe(true)
  })
  it("defaults to session storage", () => {
    sessionStorage.clear()
    expect(recoverFromChunkFailure({ reload: vi.fn(), now: () => 45_000_000 })).toBe(true)
    expect(sessionStorage.getItem("beat:chunk-recovery-at")).toBe("45000000")
  })
  it("reacts to vite preload errors", () => {
    const target = new EventTarget(), reload = vi.fn()
    installChunkRecovery(target, { storage: null, reload, now: () => 50_000_000 })
    target.dispatchEvent(new Event("vite:preloadError"))
    expect(reload).toHaveBeenCalledTimes(1)
    target.dispatchEvent(new Event("vite:preloadError"))
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
