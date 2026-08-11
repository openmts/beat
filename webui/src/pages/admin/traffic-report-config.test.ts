import { describe, expect, it, vi } from "vitest"
import {
  emptyTrafficReportForm,
  toggleTrafficReportTarget,
  trafficReportFormError,
  trafficReportFormFrom,
  trafficReportPayload,
} from "@/pages/admin/traffic-report-config"
import type { TrafficReportSchedule } from "@/types"

describe("traffic report form", () => {
  it("maps schedules and strips all-scope IDs from payloads", () => {
    const schedule: TrafficReportSchedule = {
      id: "schedule", name: "Weekly", cadence: "weekly", timezone: "Asia/Shanghai",
      send_hour: 9, send_minute: 30, weekday: 5, month_day: 1,
      all_nodes: true, node_ids: ["hidden-node"], all_channels: true, channel_ids: ["hidden-channel"],
      enabled: true, last_run_at: null, next_run_at: "", created_at: "", updated_at: "",
    }
    const form = trafficReportFormFrom(schedule)
    expect(form.weekday).toBe("5")
    expect(trafficReportPayload(form)).toMatchObject({ node_ids: [], channel_ids: [] })
  })

  it("validates schedule boundaries and explicit scopes", () => {
    vi.spyOn(Intl, "DateTimeFormat").mockReturnValue({
      resolvedOptions: () => ({ timeZone: "UTC" }),
    } as Intl.DateTimeFormat)
    const form = emptyTrafficReportForm()
    expect(form.timezone).toBe("UTC")
    expect(trafficReportFormError({ ...form, name: "" })).toBe("traffic_report.validation_name")
    expect(trafficReportFormError({ ...form, name: "Report", sendHour: "24" })).toBe("traffic_report.validation_time")
    expect(trafficReportFormError({ ...form, name: "Report", cadence: "weekly", weekday: "8" })).toBe("traffic_report.validation_weekday")
    expect(trafficReportFormError({ ...form, name: "Report", cadence: "monthly", monthDay: "0" })).toBe("traffic_report.validation_month_day")
    expect(trafficReportFormError({ ...form, name: "Report", allNodes: false })).toBe("traffic_report.validation_nodes")
    expect(trafficReportFormError({ ...form, name: "Report", allChannels: false })).toBe("traffic_report.validation_channels")
    expect(trafficReportFormError({ ...form, name: "Report" })).toBeNull()
    vi.restoreAllMocks()
  })

  it("toggles target IDs without duplicates", () => {
    expect(toggleTrafficReportTarget([], "node", true)).toEqual(["node"])
    expect(toggleTrafficReportTarget(["node"], "node", true)).toEqual(["node"])
    expect(toggleTrafficReportTarget(["node"], "node", false)).toEqual([])
  })
})
