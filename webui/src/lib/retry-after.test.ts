import { describe, expect, it } from "vitest"
import { APIError } from "@/lib/api-error"
import { maximumRetryAfterMs, pollDelay, retryAfterDelay } from "@/lib/retry-after"

describe("retry-after", () => {
  it("returns zero when the server did not ask for a delay", () => {
    expect(retryAfterDelay(new Error("boom"))).toBe(0)
    expect(retryAfterDelay(new APIError("boom", 500))).toBe(0)
    expect(pollDelay(new Error("boom"), 5_000)).toBe(5_000)
  })

  it("prefers the server delay and caps it", () => {
    expect(retryAfterDelay(new APIError("slow down", 429, "12"))).toBe(12_000)
    expect(pollDelay(new APIError("slow down", 429, "12"), 5_000)).toBe(12_000)
    expect(pollDelay(new APIError("slow down", 429, "1"), 15_000)).toBe(15_000)
    expect(retryAfterDelay(new APIError("slow down", 503, "99999"))).toBe(maximumRetryAfterMs)
    expect(retryAfterDelay(new APIError("slow down", 429, "not-a-number"))).toBe(0)
  })
})
