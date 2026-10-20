import { describe, expect, it } from "vitest"
import { dashboardMatchesQuery, dashboardTagError, parseDashboardTags, validDashboardTag } from "./workspace-dashboard-tags"
import type { DashboardDefinition } from "@/types/metric-workspace"

const base: DashboardDefinition = { version: 1, title: "Fleet health", description: "", time_range_seconds: 3600, refresh_seconds: 15, variables: [], panels: [] }

describe("dashboard search tags", () => {
  it("parses comma and whitespace separated input into lowercase keys", () => {
    expect(parseDashboardTags("Prod, hk-1  night_shift,")).toEqual(["prod", "hk-1", "night_shift"])
    expect(parseDashboardTags("   ")).toEqual([])
  })

  it("applies the same bounds as the server", () => {
    expect(dashboardTagError(["prod", "hk-1"])).toBeNull()
    expect(dashboardTagError(Array.from({ length: 9 }, (_, index) => `t${index}`))).toBe("workspace.dashboard_tags_too_many")
    expect(dashboardTagError(["Prod"])).toBe("workspace.dashboard_tag_invalid")
    expect(dashboardTagError(["-prod"])).toBe("workspace.dashboard_tag_invalid")
    expect(dashboardTagError(["a".repeat(65)])).toBe("workspace.dashboard_tag_invalid")
    expect(dashboardTagError(["prod", "prod"])).toBe("workspace.dashboard_tag_duplicate")
    expect(validDashboardTag("prod")).toBe(true)
    expect(validDashboardTag("")).toBe(false)
  })

  it("matches dashboards by title and tags only", () => {
    const definition = { ...base, tags: ["prod", "hk-1"] }
    expect(dashboardMatchesQuery(definition, "")).toBe(true)
    expect(dashboardMatchesQuery(definition, "  ")).toBe(true)
    expect(dashboardMatchesQuery(definition, "FLEET")).toBe(true)
    expect(dashboardMatchesQuery(definition, "HK")).toBe(true)
    expect(dashboardMatchesQuery(definition, "staging")).toBe(false)
    expect(dashboardMatchesQuery(base, "prod")).toBe(false)
  })
})
