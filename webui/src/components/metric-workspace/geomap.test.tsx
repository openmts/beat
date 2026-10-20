import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceGeoMap } from "./geomap"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "geomap",
    geo_locations: [{ match: "node-one", label: "机房 A", latitude: 39.9042, longitude: 116.4074 }], ...overrides }
}

function twoSeriesResult(): MetricQueryResult {
  const result = workspaceResult()
  return { ...result, series: [...result.series, { ...result.series[0], ref_id: "B", label: "memory", node_id: "node-two" }] }
}

describe("workspace geo map", () => {
  it("plots a marker per matched series on the offline base map", () => {
    const { container } = render(<LocaleProvider><WorkspaceGeoMap panel={panelOf()} result={workspaceResult()} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage · Markers" })).toBeInTheDocument()
    expect(container.querySelectorAll("[data-geomap-marker]")).toHaveLength(1)
    expect(container.querySelector("[data-geomap-land]")).not.toBeNull()
    expect(container.querySelectorAll("[data-geomap-graticule]").length).toBeGreaterThan(0)
    expect(screen.getByText("机房 A")).toBeInTheDocument()
  })

  it("renders the heat layer instead of markers when configured", () => {
    const { container } = render(<LocaleProvider><WorkspaceGeoMap panel={panelOf({ geo_layer: "heatmap" })} result={workspaceResult()} /></LocaleProvider>)
    expect(container.querySelectorAll("[data-geomap-marker]")).toHaveLength(0)
    expect(container.querySelectorAll("[data-geomap-heat]")).toHaveLength(1)
  })

  it("loads base tiles only when a template is configured", () => {
    const without = render(<LocaleProvider><WorkspaceGeoMap panel={panelOf()} result={workspaceResult()} /></LocaleProvider>)
    expect(without.container.querySelectorAll("[data-geomap-tile]")).toHaveLength(0)
    const withTiles = render(<LocaleProvider><WorkspaceGeoMap panel={panelOf({ geo_tile_url: "https://tiles.example.com/{z}/{x}/{y}.png", geo_zoom: 3 })} result={workspaceResult()} /></LocaleProvider>)
    expect(withTiles.container.querySelectorAll("[data-geomap-tile]").length).toBeGreaterThan(0)
  })

  it("reports series that have no matching location", () => {
    const { container } = render(<LocaleProvider><WorkspaceGeoMap panel={panelOf()} result={twoSeriesResult()} /></LocaleProvider>)
    expect(container.querySelectorAll("[data-geomap-marker]")).toHaveLength(1)
    expect(screen.getByText("1 series without coordinates")).toBeInTheDocument()
  })

  it("shows the localized empty state when nothing matches", () => {
    const unmatched = panelOf({ geo_locations: [{ match: "other-node", latitude: 0, longitude: 0 }] })
    const { container } = render(<LocaleProvider><WorkspaceGeoMap panel={unmatched} result={workspaceResult()} /></LocaleProvider>)
    expect(container.querySelectorAll("[data-geomap-marker]")).toHaveLength(0)
    expect(screen.getByText("No samples in this range")).toBeInTheDocument()
    expect(screen.getByText("No series matched a location in the location table.")).toBeInTheDocument()
  })
})
