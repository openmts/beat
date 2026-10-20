import { describe, expect, it } from "vitest"
import { formatWorkspaceInstant, parseWorkspaceTimeExpression } from "@/lib/workspace-time-expression"

// 2026-10-17T09:30:45Z，全部断言都用这个固定时刻，避免依赖真实时钟。
const now = Date.UTC(2026, 9, 17, 9, 30, 45)
const value = (text: string) => { const parsed = parseWorkspaceTimeExpression(text, now); return "error" in parsed ? parsed.error : parsed.value }

describe("workspace time expressions", () => {
  it("resolves relative offsets in seconds, minutes, hours, days and weeks", () => {
    expect(value("now")).toBe("2026-10-17T09:30:45Z")
    expect(value("now-30m")).toBe("2026-10-17T09:00:45Z")
    expect(value("now-6h")).toBe("2026-10-17T03:30:45Z")
    expect(value("now-2d")).toBe("2026-10-15T09:30:45Z")
    expect(value("now-1w")).toBe("2026-10-10T09:30:45Z")
    expect(value("now-90s")).toBe("2026-10-17T09:29:15Z")
  })

  it("aligns relative expressions to the requested unit", () => {
    expect(value("now/d")).toBe("2026-10-17T00:00:00Z")
    expect(value("now/h")).toBe("2026-10-17T09:00:00Z")
    expect(value("now/m")).toBe("2026-10-17T09:30:00Z")
    expect(value("now/s")).toBe("2026-10-17T09:30:45Z")
    expect(value("now-1d/d")).toBe("2026-10-16T00:00:00Z")
    expect(value("now/w")).toBe("2026-10-12T00:00:00Z")
  })

  it("accepts UTC absolute times with optional clock and zone suffix", () => {
    expect(value("2026-10-17T09:00:00Z")).toBe("2026-10-17T09:00:00Z")
    expect(value("2026-10-17 09:00")).toBe("2026-10-17T09:00:00Z")
    expect(value("2026-10-17")).toBe("2026-10-17T00:00:00Z")
    expect(value(" 2026-10-17T23:59:59 ")).toBe("2026-10-17T23:59:59Z")
  })

  it("rejects unknown words, out-of-range clocks, rolled dates and huge offsets", () => {
    for (const text of ["", "  ", "yesterday", "now+1h", "now-", "now-1y", "now-999999h", "2026-13-01", "2026-02-31", "2026-10-17T24:00", "2026-10-17T09:60", "2026-10-17T09:00:75", "17/10/2026"]) {
      expect(value(text)).toBe("workspace.expression_invalid")
    }
  })

  it("formats instants at second precision", () => {
    expect(formatWorkspaceInstant(now)).toBe("2026-10-17T09:30:45Z")
    expect(formatWorkspaceInstant(0)).toBe("1970-01-01T00:00:00Z")
  })
})
