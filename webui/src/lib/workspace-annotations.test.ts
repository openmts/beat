import { describe, expect, it } from "vitest"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import { annotationFetchWindow, workspaceAnnotationMarkers, workspaceAnnotationScopeLabel } from "./workspace-annotations"

function annotation(overrides: Partial<DashboardAnnotation> = {}): DashboardAnnotation {
  return { id: "a1", dashboard_id: "public-default", title: "Deploy", tags: [], color: "blue", visibility: "private",
    from: "2026-10-14T10:00:00Z", to: "2026-10-14T11:00:00Z",
    created_at: "2026-10-14T09:00:00Z", updated_at: "2026-10-14T09:00:00Z", ...overrides }
}

const from = Date.parse("2026-10-14T09:00:00Z")
const to = Date.parse("2026-10-14T12:00:00Z")

describe("workspace annotation markers", () => {
  it("clips dashboard annotations to the query window on every panel", () => {
    const markers = workspaceAnnotationMarkers([annotation({ from: "2026-10-14T08:00:00Z", to: "2026-10-14T10:30:00Z" })], "cpu", from, to)
    expect(markers).toHaveLength(1)
    expect(markers[0].start).toBe(from)
    expect(markers[0].end).toBe(Date.parse("2026-10-14T10:30:00Z"))
    expect(workspaceAnnotationMarkers(markers.map((item) => item.annotation), "disk", from, to)).toHaveLength(1)
  })

  it("keeps panel annotations on their own panel only", () => {
    const value = annotation({ panel_id: "cpu" })
    expect(workspaceAnnotationMarkers([value], "cpu", from, to)).toHaveLength(1)
    expect(workspaceAnnotationMarkers([value], "disk", from, to)).toHaveLength(0)
  })

  it("drops annotations outside the window or with unparsable timestamps", () => {
    const values = [
      annotation({ id: "before", from: "2026-10-14T06:00:00Z", to: "2026-10-14T07:00:00Z" }),
      annotation({ id: "after", from: "2026-10-14T13:00:00Z", to: "2026-10-14T14:00:00Z" }),
      annotation({ id: "broken", from: "not-a-time" }),
    ]
    expect(workspaceAnnotationMarkers(values, "cpu", from, to)).toHaveLength(0)
  })

  it("keeps an annotation that touches the window boundary", () => {
    const markers = workspaceAnnotationMarkers([annotation({ from: "2026-10-14T11:00:00Z", to: "2026-10-14T13:00:00Z" })], "cpu", from, to)
    expect(markers).toHaveLength(1)
    expect(markers[0].end).toBe(to)
  })

  it("orders markers by start time", () => {
    const later = annotation({ id: "later", from: "2026-10-14T11:00:00Z", to: "2026-10-14T11:30:00Z" })
    const earlier = annotation({ id: "earlier", from: "2026-10-14T09:30:00Z", to: "2026-10-14T10:00:00Z" })
    expect(workspaceAnnotationMarkers([later, earlier], "cpu", from, to).map((item) => item.annotation.id)).toEqual(["earlier", "later"])
  })

  it("returns nothing for an invalid window", () => {
    expect(workspaceAnnotationMarkers([annotation()], "cpu", to, from)).toHaveLength(0)
    expect(workspaceAnnotationMarkers([annotation()], "cpu", Number.NaN, to)).toHaveLength(0)
  })
})

describe("workspace annotation scope labels", () => {
  it("labels panel annotations with the panel title and falls back to the id", () => {
    expect(workspaceAnnotationScopeLabel(annotation(), { cpu: "CPU usage" })).toBeNull()
    expect(workspaceAnnotationScopeLabel(annotation({ panel_id: "cpu" }), { cpu: "CPU usage" })).toBe("CPU usage")
    expect(workspaceAnnotationScopeLabel(annotation({ panel_id: "disk" }), { cpu: "CPU usage" })).toBe("disk")
  })
})

describe("annotation fetch window", () => {
  const domain = { from: "2026-10-14T09:00:00.000Z", to: "2026-10-14T12:00:00.000Z" }

  it("widens the window to cover the largest panel time shift", () => {
    expect(annotationFetchWindow(domain, [])).toEqual(domain)
    expect(annotationFetchWindow(domain, [{ id: "cpu" } as never])).toEqual(domain)
    expect(annotationFetchWindow(domain, [{ id: "cpu", time_shift: "1d" } as never]))
      .toEqual({ from: "2026-10-13T09:00:00.000Z", to: "2026-10-13T12:00:00.000Z" })
    expect(annotationFetchWindow(domain, [{ id: "cpu", time_shift: "1h" } as never, { id: "disk", time_shift: "3h" } as never]))
      .toEqual({ from: "2026-10-14T06:00:00.000Z", to: "2026-10-14T09:00:00.000Z" })
    expect(annotationFetchWindow(domain, [{ id: "cpu", time_shift: "1w" } as never])).toEqual(domain)
  })
})
