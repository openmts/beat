import { afterEach, describe, expect, it, vi } from "vitest"
import type { ChunkStorage } from "@/lib/chunk-recovery"
import { checkForNewBuild, installVersionWatch, runningEntry, servedEntry, type VersionResponse, type VersionWatchDeps } from "@/lib/version-watch"

type StubStorage = ChunkStorage & { value: () => string | null }

function memoryStorage(): StubStorage {
  let stored: string | null = null
  return { getItem: () => stored, setItem: (_key, value) => { stored = value }, value: () => stored }
}

function html(entry: string, order: "type-first" | "src-first" = "type-first"): string {
  return order === "type-first"
    ? `<!doctype html><html><head><script type="module" crossorigin src="${entry}"></script></head><body></body></html>`
    : `<!doctype html><html><head><script crossorigin src="${entry}" type="module"></script></head><body></body></html>`
}

function respond(body: string, ok = true): VersionResponse {
  return { ok, text: () => Promise.resolve(body) }
}

function attachRunning(entry: string): () => void {
  const script = document.createElement("script")
  script.type = "module"
  script.setAttribute("src", entry)
  document.head.append(script)
  return () => script.remove()
}

const hidden = { visibilityState: "hidden", querySelector: () => null } as unknown as Document

afterEach(() => {
  document.head.querySelectorAll('script[type="module"]').forEach((node) => node.remove())
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("version watch", () => {
  it("reads the running module script and both served attribute orders", () => {
    const detach = attachRunning("/assets/index-V1.js")
    expect(runningEntry(document)).toBe("/assets/index-V1.js")
    expect(servedEntry(html("/assets/index-V2.js"))).toBe("/assets/index-V2.js")
    expect(servedEntry(html("/assets/index-V3.js", "src-first"))).toBe("/assets/index-V3.js")
    detach()
    expect(runningEntry(document)).toBeNull()
  })

  it("ignores markup without a module script", () => {
    expect(servedEntry('<html><head><script src="/assets/other.js"></script></head></html>')).toBeNull()
    expect(servedEntry('<script type="module"></script>')).toBeNull()
  })

  it("reloads once when the served entry differs", async () => {
    const detach = attachRunning("/assets/index-T1-old.js")
    const reload = vi.fn()
    const deps: VersionWatchDeps = { storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-T1-new.js"))) }
    expect(await checkForNewBuild(deps)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    detach()
  })

  it("keeps the loaded page when the served entry matches", async () => {
    const detach = attachRunning("/assets/index-T2.js")
    const reload = vi.fn()
    const same = await checkForNewBuild({ storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-T2.js"))) })
    const absolute = await checkForNewBuild({ storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond(html("http://192.168.1.48:8081/assets/index-T2.js"))) })
    expect(same).toBe(false)
    expect(absolute).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    detach()
  })

  it("never reloads twice for the same served entry", async () => {
    const detach = attachRunning("/assets/index-T3-old.js")
    const storage = memoryStorage()
    const reload = vi.fn()
    const deps: VersionWatchDeps = { storage, reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-T3-new.js"))) }
    expect(await checkForNewBuild(deps)).toBe(true)
    expect(await checkForNewBuild(deps)).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
    expect(storage.value()).toContain("index-T3-new.js")
    detach()
  })

  it("falls back to the in-memory record without storage", async () => {
    const detach = attachRunning("/assets/index-T4-old.js")
    const reload = vi.fn()
    const deps: VersionWatchDeps = { storage: null, reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-T4-new.js"))) }
    expect(await checkForNewBuild(deps)).toBe(true)
    expect(await checkForNewBuild(deps)).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
    detach()
  })

  it("stays idle when the document is hidden or unreadable", async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(respond(html("/assets/index-T5.js"))))
    expect(await checkForNewBuild({ document: hidden, fetchImpl, reload: vi.fn() })).toBe(false)
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(await checkForNewBuild({ storage: memoryStorage(), fetchImpl, reload: vi.fn() })).toBe(false)
  })

  it("keeps the page when the probe fails", async () => {
    const detach = attachRunning("/assets/index-T6.js")
    const reload = vi.fn()
    expect(await checkForNewBuild({ storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond("", false)) })).toBe(false)
    expect(await checkForNewBuild({ storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond("<html></html>")) })).toBe(false)
    expect(await checkForNewBuild({ storage: memoryStorage(), reload, fetchImpl: () => Promise.reject(new Error("offline")) })).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    detach()
  })

  it("probes again when the browser restores the page from the back-forward cache", async () => {
    const detach = attachRunning("/assets/index-T8-old.js")
    const fetchImpl = vi.fn(() => Promise.resolve(respond(html("/assets/index-T8-new.js"))))
    const reload = vi.fn()
    const stop = installVersionWatch({ storage: memoryStorage(), reload, fetchImpl, intervalMs: 3_600_000 })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => {
      expect(fetchImpl).toHaveBeenCalledTimes(1)
      expect(reload).toHaveBeenCalledTimes(1)
    })
    window.dispatchEvent(new Event("pageshow"))
    await vi.waitFor(() => { expect(fetchImpl).toHaveBeenCalledTimes(2) })
    stop()
    window.dispatchEvent(new Event("pageshow"))
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(reload).toHaveBeenCalledTimes(1)
    detach()
  })

  it("probes on install, visibility changes and the interval until cleanup", async () => {
    vi.useFakeTimers()
    const detach = attachRunning("/assets/index-T7-old.js")
    const reload = vi.fn()
    const fetchImpl = vi.fn(() => Promise.resolve(respond(html("/assets/index-T7-new.js"))))
    const stop = installVersionWatch({ storage: memoryStorage(), reload, fetchImpl, intervalMs: 1_000 })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    document.dispatchEvent(new Event("visibilitychange"))
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchImpl).toHaveBeenCalledTimes(3)
    expect(reload).toHaveBeenCalledTimes(1)
    stop()
    document.dispatchEvent(new Event("visibilitychange"))
    await vi.advanceTimersByTimeAsync(2_000)
    expect(fetchImpl).toHaveBeenCalledTimes(3)
    detach()
  })

  it("holds the switch while the user is editing or a dialog is open", async () => {
    const detach = attachRunning("/assets/index-T9-old.js")
    const reload = vi.fn()
    const deps: VersionWatchDeps = { storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-T9-new.js"))) }
    const input = document.createElement("input")
    document.body.append(input)
    input.focus()
    expect(await checkForNewBuild(deps)).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    input.blur()
    expect(await checkForNewBuild(deps)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    input.remove()
    detach()
  })

  it("holds the switch while a dialog is open", async () => {
    const detach = attachRunning("/assets/index-TA-old.js")
    const reload = vi.fn()
    const dialog = document.createElement("div")
    dialog.setAttribute("role", "dialog")
    document.body.append(dialog)
    const deps: VersionWatchDeps = { storage: memoryStorage(), reload, fetchImpl: () => Promise.resolve(respond(html("/assets/index-TA-new.js"))) }
    expect(await checkForNewBuild(deps)).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    dialog.remove()
    expect(await checkForNewBuild(deps)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    detach()
  })

  it("probes the served entry once a minute by default", async () => {
    vi.useFakeTimers()
    const detach = attachRunning("/assets/index-TB.js")
    const fetchImpl = vi.fn(() => Promise.resolve(respond(html("/assets/index-TB.js"))))
    const stop = installVersionWatch({ storage: memoryStorage(), reload: vi.fn(), fetchImpl })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(59_000)
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    stop()
    detach()
  })
})
