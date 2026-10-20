import { describe, expect, it } from "vitest"
import { defaultFleetControls, fleetControlsQuery, metricFleetSort, parseFleetControls } from "./fleet-state"
import { formatPublicPrice, microsToPrice, priceToMicros, validCalendarDate } from "./lifecycle-format"
describe("bounded fleet presentation state", () => {
 it("round trips filters while bounding duplicates, search and input", () => {
  const expected = { ...defaultFleetControls, search: "東京", groups: ["one"], tags: ["edge"], country: "JP", status: "online", freshness: "fresh", expiry: "7", sort: "cpu" as const, direction: "desc" as const, offline: "first" as const, layout: "table" as const }
  expect(parseFleetControls(fleetControlsQuery(expected))).toEqual(expected)
  expect(parseFleetControls("?bad=" + "a".repeat(4096))).toEqual(defaultFleetControls)
  expect(parseFleetControls("?q=" + "x".repeat(150)).search).toHaveLength(128)
  expect(parseFleetControls("?group=a&group=a&group=&country=us&sort=evil")).toMatchObject({ groups: ["a"], country: "", sort: "order" })
  expect(parseFleetControls(Array.from({ length: 20 }, (_, i) => `tag=${i}`).join("&")).tags).toHaveLength(16)
  expect(fleetControlsQuery(defaultFleetControls)).toBe("")
  expect(metricFleetSort("memory")).toBe(true); expect(metricFleetSort("expiry")).toBe(false)
 })
 it("preserves exact int64 money and rejects malformed calendar dates", () => {
  expect(priceToMicros("9223372036854.775807")).toBe("9223372036854775807")
  expect(microsToPrice("9223372036854775807")).toBe("9223372036854.775807")
  expect(priceToMicros("  ")).toBeNull(); expect(microsToPrice(null)).toBe(""); expect(microsToPrice("1000000")).toBe("1")
  for (const value of ["-1", "1e3", "0.0000001", "9223372036854.775808"]) expect(() => priceToMicros(value)).toThrow()
  for (const value of ["-1", "abc", "9223372036854775808"]) expect(() => microsToPrice(value)).toThrow()
  expect(formatPublicPrice("1234567", "USD", "en-US")).toBe("$1.234567")
  expect(formatPublicPrice("9223372036854775807", "USD", "en-US")).toBe("$9,223,372,036,854.775807")
  expect(formatPublicPrice(undefined, "USD", "en")).toBeNull(); expect(formatPublicPrice("1", "bad", "en")).toBeNull(); expect(formatPublicPrice("bad", "USD", "en")).toBeNull()
  expect(validCalendarDate("2024-02-29")).toBe(true); expect(validCalendarDate("")).toBe(true)
  for (const value of ["2023-02-29", "01-01-2024", "2024-13-01"]) expect(validCalendarDate(value)).toBe(false)
 })
})
