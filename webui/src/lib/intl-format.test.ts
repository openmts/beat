import { describe, expect, it } from "vitest"
import { dateTimeFormatter, formatBytes, formatDate, formatDateTime, formatDecimal, formatInteger, formatNumber, formatPercent, formatTime, formatTimeWithSeconds, numberFormatter } from "./intl-format"

const moment = Date.parse("2025-10-15T04:00:00.000Z")

describe("locale-aware date and time", () => {
  it("renders the application locale instead of the browser default", () => {
    const english = formatDateTime("en", moment)
    const simplified = formatDateTime("zh-CN", moment)
    expect(english).not.toBe(simplified)
    expect(simplified).toContain("2025")
  })

  it("covers date, clock and second-level variants", () => {
    expect(formatDate("en", moment)).not.toBe("")
    expect(formatTime("en", moment)).not.toContain("2025")
    expect(formatTimeWithSeconds("en", moment).split(":").length).toBeGreaterThanOrEqual(3)
  })

  it("returns a dash for missing or invalid timestamps", () => {
    for (const value of [null, undefined, "", "not-a-date"]) {
      expect(formatDateTime("en", value)).toBe("—")
      expect(formatDate("en", value)).toBe("—")
      expect(formatTime("en", value)).toBe("—")
    }
  })

  it("caches formatters per locale and options", () => {
    expect(dateTimeFormatter("en")).toBe(dateTimeFormatter("en"))
    expect(dateTimeFormatter("en")).not.toBe(dateTimeFormatter("zh-CN"))
    expect(numberFormatter("en", { maximumFractionDigits: 1 })).toBe(numberFormatter("en", { maximumFractionDigits: 1 }))
  })
})

describe("locale-aware numbers", () => {
  it("formats integers, decimals and percentages", () => {
    expect(formatInteger("en", 4096)).toBe("4,096")
    expect(formatDecimal("en", 2.25, 1)).toBe("2.3")
    expect(formatDecimal("en", 2, 1)).toBe("2")
    expect(formatPercent("en", 85.25, 1)).toBe("85.3%")
    expect(formatPercent("en", 100, 1)).toBe("100%")
  })

  it("returns a dash for non-finite numbers", () => {
    expect(formatNumber("en", Number.NaN)).toBe("—")
    expect(formatNumber("en", null)).toBe("—")
    expect(formatInteger("en", Number.POSITIVE_INFINITY)).toBe("—")
    expect(formatPercent("en", Number.NaN)).toBe("—")
  })

  it("honours explicit digit bounds", () => {
    expect(formatNumber("en", 1.23456, { maximumFractionDigits: 2 })).toBe("1.23")
    expect(formatNumber("en", 1, { minimumFractionDigits: 2, maximumFractionDigits: 2 })).toBe("1.00")
  })

  it("rounds counts the same way for every locale family", () => {
    for (const locale of ["en", "zh-CN", "ja", "id"]) expect(formatInteger(locale, 1.6)).toBe(formatInteger(locale, 2))
  })
})

describe("locale-aware byte sizes", () => {
  it("scales by 1024 and keeps the documented unit symbols", () => {
    expect(formatBytes("en", 512)).toBe("512 B")
    expect(formatBytes("en", 1536)).toBe("1.5 KiB")
    expect(formatBytes("en", 1024 * 1024)).toBe("1 MiB")
    expect(formatBytes("en", -2048)).toBe("-2 KiB")
    expect(formatBytes("en", 1024 ** 3)).toBe("1 GiB")
  })

  it("caps the displayed unit and appends the rate suffix", () => {
    expect(formatBytes("en", 5 * 1024 ** 3, { maximumUnit: "MiB" })).toBe("5,120 MiB")
    expect(formatBytes("en", 2048, { maximumUnit: "MiB", perSecond: true })).toBe("2 KiB/s")
    expect(formatBytes("en", 1024 ** 5, { maximumUnit: "GiB" })).toBe("1,048,576 GiB")
  })

  it("supports two decimals and rejects unusable input", () => {
    expect(formatBytes("en", 1536, { decimals: 2 })).toBe("1.5 KiB")
    expect(formatBytes("en", 1024 + 5, { decimals: 2 })).toBe("1 KiB")
    expect(formatBytes("en", Number.NaN)).toBe("—")
    expect(formatBytes("en", null)).toBe("—")
  })
})
