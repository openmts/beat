import { describe, expect, it } from "vitest"
import { APIError } from "@/lib/api-error"
import { workspaceIdleRecoveryIntervalMs, workspaceRecoveryDelay, workspaceRecoveryIntervalMs, workspaceRetryDelay } from "@/lib/workspace-retry"

describe("workspace retry policy", () => {
  it("retries transport failures, throttling and server errors with growing delays", () => {
    expect(workspaceRetryDelay(new APIError("network", 0), 0)).toBe(400)
    expect(workspaceRetryDelay(new APIError("timeout", 408), 1)).toBe(1_000)
    expect(workspaceRetryDelay(new APIError("too early", 425), 2)).toBe(2_500)
    expect(workspaceRetryDelay(new APIError("throttled", 429), 5)).toBe(12_000)
    expect(workspaceRetryDelay(new APIError("unavailable", 503), 3)).toBe(5_000)
  })
  it("honours retry-after for throttled responses without exceeding the ceiling", () => {
    expect(workspaceRetryDelay(new APIError("throttled", 429, "3"), 0)).toBe(3_000)
    expect(workspaceRetryDelay(new APIError("throttled", 429, "90"), 0)).toBe(30_000)
    expect(workspaceRetryDelay(new APIError("throttled", 429, "not-a-number"), 0)).toBe(400)
  })
  it("stops on deterministic client errors and keeps local validation failures retryable", () => {
    expect(workspaceRetryDelay(new APIError("not found", 404), 0)).toBeNull()
    expect(workspaceRetryDelay(new APIError("forbidden", 403), 0)).toBeNull()
    expect(workspaceRetryDelay(new APIError("bad request", 400), 0)).toBeNull()
    expect(workspaceRetryDelay(new Error("workspace.load_failed"), 0)).toBe(400)
  })
  it("stops once the retry budget is exhausted", () => {
    expect(workspaceRetryDelay(new APIError("down", 503), 5)).toBe(12_000)
    expect(workspaceRetryDelay(new APIError("down", 503), 6)).toBeNull()
    expect(workspaceRetryDelay(new APIError("down", 503), 99)).toBeNull()
  })
  it("keeps retrying retryable failures after the fast budget is exhausted", () => {
    expect(workspaceRecoveryDelay(new APIError("down", 503), 0)).toBe(workspaceRecoveryIntervalMs)
    expect(workspaceRecoveryDelay(new APIError("network", 0), 14)).toBe(workspaceRecoveryIntervalMs)
  })
  it("honours a longer retry-after while recovering, then keeps a slower cadence", () => {
    expect(workspaceRecoveryDelay(new APIError("throttled", 429, "45"), 0)).toBe(30_000)
    expect(workspaceRecoveryDelay(new APIError("throttled", 429, "1"), 0)).toBe(workspaceRecoveryIntervalMs)
    // 升级窗口长于慢速预算时页面必须继续自愈：预算用尽后降到每分钟一次，而不是永久停在错误态。
    expect(workspaceRecoveryDelay(new APIError("down", 503), 15)).toBe(workspaceIdleRecoveryIntervalMs)
    expect(workspaceRecoveryDelay(new APIError("down", 503), 999)).toBe(workspaceIdleRecoveryIntervalMs)
  })
  it("never slow-retries deterministic client failures", () => {
    expect(workspaceRecoveryDelay(new APIError("not found", 404), 0)).toBeNull()
    expect(workspaceRecoveryDelay(new APIError("unauthorized", 401), 0)).toBeNull()
    expect(workspaceRecoveryDelay(new APIError("forbidden", 403), 0)).toBeNull()
  })
})
