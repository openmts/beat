import { describe, expect, it } from "vitest"
import { normalizeCountryCode } from "@/lib/country-code"

describe("country code", () => {
  it("normalizes only uppercase ISO alpha-2 codes", () => {
    expect(normalizeCountryCode("JP")).toBe("JP")
    expect(normalizeCountryCode("US")).toBe("US")
    for (const value of ["jp", "JPN", " J", "1P", "", undefined, null, 42, {}]) expect(normalizeCountryCode(value)).toBe("")
  })
})
