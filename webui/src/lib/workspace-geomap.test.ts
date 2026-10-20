import { describe, expect, it } from "vitest"
import {
  defaultGeoMarkerSize, defaultGeoZoom, emptyGeoLocation, geoError, geoHeatCells, geoLayer, geoMarkerRadius, geoMarkers,
  geoMaxLatitude, geoPatch, geoTiles, geoUnmatchedSeries, geoViewport, geoWorldSize, geoZoom, maxGeoLocations, maxGeoZoom,
  minGeoMarkerSize, minGeoZoom, validGeoTileURL,
} from "./workspace-geomap"
import type { GeoMapPanel } from "@/lib/workspace-geomap"
import type { MetricQueryResult } from "@/types/metric-workspace"

function geoPanel(panel: Partial<GeoMapPanel> = {}): GeoMapPanel {
  return { type: "geomap", geo_locations: [{ match: "n1", latitude: 39.9042, longitude: 116.4074 }], thresholds: [], series: [], ...panel }
}

function resultWith(series: { ref_id: string; node_id: string; device?: string; value?: number | null }[]): MetricQueryResult {
  return { from: "2026-10-16T00:00:00Z", to: "2026-10-16T01:00:00Z", interval_ms: 1000,
    series: series.map((entry) => ({ ref_id: entry.ref_id, node_id: entry.node_id, ...(entry.device ? { device_id: entry.device } : {}),
      metric: "cpu", aggregation: "avg", label: entry.ref_id, unit: "percent", available_from: null, state: "ok",
      points: [{ timestamp: 1_000_000, value: entry.value ?? 42 }] })) }
}

describe("workspace geo map", () => {
  it("falls back to the documented defaults", () => {
    expect(geoLayer({ type: "geomap" })).toBe("markers")
    expect(geoZoom({ type: "geomap" })).toBe(defaultGeoZoom)
    expect(geoMarkerRadius({ type: "geomap" }).valueOf()).toBeGreaterThan(0)
    expect(geoLayer({ type: "geomap", geo_layer: "polygons" as unknown as "markers" })).toBe("markers")
    expect(geoZoom({ type: "geomap", geo_zoom: maxGeoZoom + 1 })).toBe(defaultGeoZoom)
    expect(geoError(geoPanel({ geo_marker_size: minGeoMarkerSize - 1 }))).toBe("workspace.geomap_marker_invalid")
  })

  it("projects coordinates into the mercator world square", () => {
    const equator = geoViewport({ type: "geomap" })
    expect(equator).toEqual({ x: 0, y: 0, size: geoWorldSize })
    const center = geoMarkers(geoPanel({ geo_center_latitude: 0, geo_center_longitude: 0 }), resultWith([{ ref_id: "A", node_id: "n1" }]))
    // 北京在东北半球：x 落在方块右半，y 落在方块上半。
    expect(center[0]!.x).toBeGreaterThan(geoWorldSize / 2)
    expect(center[0]!.y).toBeLessThan(geoWorldSize / 2)
    expect(geoViewport({ type: "geomap", geo_zoom: 2, geo_center_latitude: 0, geo_center_longitude: 0 })).toEqual({ x: 135, y: 135, size: 90 })
  })

  it("matches series by node id, device id and node name", () => {
    const panel = geoPanel({ geo_locations: [
      { match: "node-a", latitude: 0, longitude: 0 },
      { match: "gpu-0", latitude: 10, longitude: 10 },
      { match: "北京节点", latitude: 20, longitude: 20 },
    ] })
    const result = resultWith([{ ref_id: "A", node_id: "node-a" }, { ref_id: "B", node_id: "unused", device: "gpu-0" }, { ref_id: "C", node_id: "missing" }])
    const markers = geoMarkers(panel, result, [{ ref_id: "C", node_id: "missing", node_name: "北京节点", label: "C", color: "blue", metric: "cpu", aggregation: "avg" }])
    expect(markers.map((marker) => marker.label)).toEqual(["node-a", "gpu-0", "北京节点"])
    expect(geoUnmatchedSeries(panel, result)).toBe(1)
  })

  it("colours markers by threshold when thresholds are configured", () => {
    const panel = geoPanel({ thresholds: [{ value: 50, color: "rose" }], series: [{ ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", label: "", color: "green" }] })
    const hot = geoMarkers(panel, resultWith([{ ref_id: "A", node_id: "n1", value: 80 }]))
    expect(hot[0]!.color).toBe("#e11d48")
    const cold = geoMarkers(geoPanel({ series: panel.series }), resultWith([{ ref_id: "A", node_id: "n1", value: 80 }]))
    expect(cold[0]!.color).toBe("#16a34a")
  })

  it("builds heat cells relative to the viewport", () => {
    const panel = geoPanel({ geo_zoom: 4, geo_center_latitude: 39.9042, geo_center_longitude: 116.4074 })
    const markers = geoMarkers(panel, resultWith([{ ref_id: "A", node_id: "n1", value: 80 }]))
    const cells = geoHeatCells(panel, markers)
    expect(cells).toHaveLength(1)
    expect(cells[0]!.count).toBe(1)
    expect(cells[0]!.ratio).toBe(1)
    expect(cells[0]!.value).toBe(80)
    // 视口外的标记不进热力网格。
    expect(geoHeatCells(geoPanel({ geo_zoom: 4, geo_center_latitude: -40, geo_center_longitude: -100 }), markers)).toEqual([])
  })

  it("renders tiles only for a valid template inside the viewport", () => {
    expect(geoTiles({ type: "geomap" })).toEqual([])
    // 缩放 1 的视口是 180x180 世界单位、以赤道与本初子午线为中心，因此跨 2x2 张瓦片。
    expect(geoTiles({ type: "geomap", geo_tile_url: "https://tiles.example.com/{z}/{x}/{y}.png", geo_zoom: 1 }).map((tile) => tile.key))
      .toEqual(["1/0/0", "1/1/0", "1/0/1", "1/1/1"])
    const tiles = geoTiles({ type: "geomap", geo_tile_url: "http://t/{z}/{x}/{y}.png", geo_zoom: 2, geo_center_latitude: 0, geo_center_longitude: 0 })
    expect(tiles.map((tile) => tile.key)).toEqual(["2/1/1", "2/2/1", "2/1/2", "2/2/2"])
  })

  it("mirrors the server side validation of every geo field", () => {
    expect(geoError({ type: "geomap" })).toBe("workspace.geomap_locations_required")
    expect(geoError({ type: "timeseries", geo_zoom: 1 })).toBe("workspace.geomap_only")
    expect(geoError({ type: "timeseries" })).toBeNull()
    expect(geoError({ type: "geomap", geo_locations: [{ match: " n1 ", latitude: 1, longitude: 2 }, { match: "N1", latitude: 3, longitude: 4 }] }))
      .toBe("workspace.geomap_location_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "", latitude: 1, longitude: 2 }] })).toBe("workspace.geomap_location_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: geoMaxLatitude + 1, longitude: 2 }] })).toBe("workspace.geomap_location_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 181 }] })).toBe("workspace.geomap_location_invalid")
    expect(geoError({ type: "geomap", geo_locations: Array.from({ length: maxGeoLocations + 1 }, (_, index) => ({ match: `n${index}`, latitude: 1, longitude: 2 })) }))
      .toBe("workspace.geomap_locations_limit")
    expect(geoError({ type: "geomap", geo_locations: [], geo_layer: "polygons" as unknown as "markers" })).toBe("workspace.geomap_locations_required")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 2 }], geo_layer: "polygons" as unknown as "markers" }))
      .toBe("workspace.geomap_layer_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 2 }], geo_zoom: maxGeoZoom + 1 })).toBe("workspace.geomap_zoom_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 2 }], geo_center_latitude: 10 })).toBe("workspace.geomap_center_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 2 }], geo_center_latitude: 10, geo_center_longitude: 181 }))
      .toBe("workspace.geomap_center_invalid")
    expect(geoError({ type: "geomap", geo_locations: [{ match: "n1", latitude: 1, longitude: 2 }], geo_tile_url: "ftp://t/{z}/{x}/{y}.png" }))
      .toBe("workspace.geomap_tile_invalid")
    expect(geoError(geoPanel({ geo_zoom: minGeoZoom, geo_marker_size: 12, geo_tile_url: "https://t/{z}/{x}/{y}.png" }))).toBeNull()
  })

  it("validates tile templates like the server does", () => {
    expect(validGeoTileURL(undefined)).toBe(true)
    expect(validGeoTileURL("")).toBe(true)
    expect(validGeoTileURL("https://t/{z}/{x}/{y}.png")).toBe(true)
    expect(validGeoTileURL("https://t/{z}/{x}.png")).toBe(false)
    expect(validGeoTileURL("https://t/{z}/{x}/{y}/{s}.png")).toBe(false)
    expect(validGeoTileURL("javascript:alert({z}{x}{y})")).toBe(false)
    expect(validGeoTileURL(`https://t/${"a".repeat(600)}/{z}/{x}/{y}.png`)).toBe(false)
  })

  it("clears geo fields only when the panel stops being a geo map", () => {
    expect(geoPatch("geomap")).toEqual({})
    const cleared = geoPatch("timeseries")
    expect(Object.values(cleared).every((value) => value === undefined)).toBe(true)
    expect(Object.keys(cleared)).toHaveLength(7)
    expect(emptyGeoLocation()).toEqual({ match: "", latitude: 0, longitude: 0 })
    expect(defaultGeoMarkerSize).toBe(8)
    expect(minGeoMarkerSize).toBe(2)
  })
})
