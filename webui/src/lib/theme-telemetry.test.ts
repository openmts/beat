import { describe, expect, it } from "vitest"
import { themeTelemetry } from "@/lib/theme-telemetry"
import { workspaceNode } from "@/test/metric-workspace-fixtures"

describe("public telemetry snapshot whitelist", () => {
  it("supports raw node responses without freshness and copies only confirmed public fields", () => {
    expect(themeTelemetry(workspaceNode())).toEqual({})
    expect(themeTelemetry({ ...workspaceNode(), telemetry_state: "unavailable", telemetry_at: null, telemetry_age_seconds: null })).toEqual({ telemetry_state: "unavailable", telemetry_at: null, telemetry_age_seconds: null })
    expect(themeTelemetry({ ...workspaceNode(), telemetry_state: "fresh", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: 0 })).toEqual({ telemetry_state: "fresh", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: 0 })
  })
  it.each([
    { telemetry_state: "unknown", telemetry_at: null, telemetry_age_seconds: null },
    { telemetry_state: "fresh", telemetry_at: null, telemetry_age_seconds: 1 },
    { telemetry_state: "fresh", telemetry_at: "invalid", telemetry_age_seconds: 1 },
    { telemetry_state: "fresh", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: -1 },
    { telemetry_state: "fresh", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: 1.5 },
    { telemetry_state: "stale", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: NaN },
    { telemetry_state: "unavailable", telemetry_at: "2026-09-13T00:00:00Z", telemetry_age_seconds: 0 },
  ])("rejects malformed or inconsistent freshness: %j", (fields) => {
    expect(() => themeTelemetry({ ...workspaceNode(), ...fields } as never)).toThrow("invalid_data")
  })
})
