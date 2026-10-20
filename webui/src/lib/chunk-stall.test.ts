import { afterEach, describe, expect, it, vi } from "vitest"
import { STALLED_CHUNK_GRACE_MS, watchStalledChunk } from "./chunk-stall"


type Timer = { handler: () => void; timeout: number }

function timerRecorder() {
  const timers: Timer[] = [], cleared: number[] = []
  return {
    timers, cleared,
    setTimer: (handler: () => void, timeout: number) => { timers.push({ handler, timeout }); return timers.length },
    clearTimer: (timer: number) => { cleared.push(timer) },
  }
}

afterEach(() => vi.useRealTimers())

describe("stalled chunk watchdog", () => {
  it("recovers once after the configured grace period", () => {
    const clock = timerRecorder(), reload = vi.fn()
    watchStalledChunk({ ...clock, graceMs: 2_500, storage: null, reload, now: () => 100_000_000 })
    expect(clock.timers).toHaveLength(1)
    expect(clock.timers[0].timeout).toBe(2_500)
    expect(reload).not.toHaveBeenCalled()
    clock.timers[0].handler()
    expect(reload).toHaveBeenCalledTimes(1)
  })
  it("waits the default grace period when none is configured", () => {
    const clock = timerRecorder()
    watchStalledChunk({ ...clock, storage: null, reload: vi.fn(), now: () => 110_000_000 })
    expect(clock.timers[0].timeout).toBe(STALLED_CHUNK_GRACE_MS)
  })
  it("cancels the watchdog when the loading state ends", () => {
    const clock = timerRecorder(), reload = vi.fn()
    const stop = watchStalledChunk({ ...clock, graceMs: 1_000, storage: null, reload, now: () => 120_000_000 })
    stop()
    expect(clock.cleared).toEqual([1])
    expect(reload).not.toHaveBeenCalled()
  })
  it("falls back to window timers without injected ones", () => {
    vi.useFakeTimers()
    const reload = vi.fn()
    const stop = watchStalledChunk({ graceMs: 5, storage: null, reload, now: () => 130_000_000 })
    vi.advanceTimersByTime(4)
    expect(reload).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(reload).toHaveBeenCalledTimes(1)
    stop()
  })
  it("keeps the current page inside the recovery guard window", () => {
    vi.useFakeTimers()
    const reload = vi.fn()
    watchStalledChunk({ graceMs: 5, storage: null, reload, now: () => 140_000_000 })
    vi.advanceTimersByTime(5)
    watchStalledChunk({ graceMs: 5, storage: null, reload, now: () => 140_000_001 })
    vi.advanceTimersByTime(5)
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
