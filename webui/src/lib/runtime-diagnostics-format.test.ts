import { expect, it } from "vitest"
import {
  clampRuntimeDuration, formatRuntimeBytes, formatRuntimePause, formatRuntimeUptime,
  runtimeDurationBounds, runtimeKindIsTimed, runtimeLabel, runtimeStatusLabelKeys,
  runtimeCaptureKinds, runtimeFailureLabelKeys,
} from "@/lib/runtime-diagnostics-format"

/** 测试翻译器把键与参数原样拼出来，既能断言取到哪个键，也能断言插值参数。 */
const t = (key: string, params?: Record<string, string | number>) => params ? `${key}:${JSON.stringify(params)}` : key

it("labels known catalog values and falls back to the raw backend identifier", () => {
  expect(runtimeLabel(t, runtimeStatusLabelKeys, "running")).toBe("runtime.status.running")
  expect(runtimeLabel(t, runtimeStatusLabelKeys, "queued")).toBe("queued")
  expect(runtimeLabel(t, runtimeFailureLabelKeys, "rate_limited")).toBe("runtime.failure.rate_limited")
  expect(runtimeLabel(t, {}, "size_limit")).toBe("size_limit")
})

it("keeps the capture kinds in the documented order", () => {
  expect(runtimeCaptureKinds).toEqual(["cpu", "trace", "heap", "allocs", "goroutine", "mutex", "block", "threadcreate"])
  expect(runtimeCaptureKinds.filter(runtimeKindIsTimed)).toEqual(["cpu", "trace"])
  expect(runtimeKindIsTimed("heap")).toBe(false)
})

it("mirrors the server duration bounds and clamps what the form may send", () => {
  expect(runtimeDurationBounds("cpu")).toEqual({ min: 5, max: 30 })
  expect(runtimeDurationBounds("trace")).toEqual({ min: 1, max: 5 })
  expect(runtimeDurationBounds("heap")).toEqual({ min: 0, max: 0 })

  expect(clampRuntimeDuration("cpu", Number.NaN)).toBe(5)
  expect(clampRuntimeDuration("cpu", 1)).toBe(5)
  expect(clampRuntimeDuration("cpu", 99)).toBe(30)
  expect(clampRuntimeDuration("trace", 2.6)).toBe(3)
  expect(clampRuntimeDuration("heap", 12)).toBe(0)
})

it("formats memory, uptime and pause with the documented fallbacks", () => {
  expect(formatRuntimeBytes(0)).toBe("0 B")
  expect(formatRuntimeBytes(-5)).toBe("0 B")
  expect(formatRuntimeBytes(Number.POSITIVE_INFINITY)).toBe("0 B")
  expect(formatRuntimeBytes(1536)).toBe("1.5 KiB")
  expect(formatRuntimeBytes(3 * 1024 ** 3, "en")).toBe("3 GiB")

  expect(formatRuntimeUptime(0, t)).toBe("runtime.uptime_just_started")
  expect(formatRuntimeUptime(90061, t)).toBe('runtime.uptime_days:{"days":1,"hours":1}')
  expect(formatRuntimeUptime(3661, t)).toBe('runtime.uptime_hours:{"hours":1,"minutes":1}')
  expect(formatRuntimeUptime(61, t)).toBe('runtime.uptime_minutes:{"minutes":1,"seconds":1}')

  expect(formatRuntimePause(0, "en")).toBe("0 ms")
  expect(formatRuntimePause(Number.NaN, "en")).toBe("0 ms")
  expect(formatRuntimePause(0.0015, "en")).toBe("1.5 ms")
})
