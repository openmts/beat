import { describe, expect, it } from "vitest"
import { GEO_LAND_GRID, GEO_LAND_RUNS, GEO_LAND_SOURCE, geoLandPath } from "@/lib/workspace-geo-land"

// 底图是生成产物：这里的断言守住「坐标系、排序、体积」三条不变量，
// 重新生成（python3 webui/scripts/generate-geo-land.py）后必须仍然通过。
describe("workspace geo land", () => {
  it("uses a usable raster resolution", () => {
    expect(Number.isInteger(GEO_LAND_GRID)).toBe(true)
    expect(GEO_LAND_GRID).toBeGreaterThanOrEqual(96)
    expect(GEO_LAND_GRID % 8).toBe(0)
  })

  it("keeps every run inside the grid and ordered per row", () => {
    expect(GEO_LAND_RUNS.length).toBeGreaterThan(100)
    const lastColumn = new Map<number, number>()
    for (const [row, columnStart, length] of GEO_LAND_RUNS) {
      expect(Number.isInteger(row)).toBe(true)
      expect(Number.isInteger(columnStart)).toBe(true)
      expect(Number.isInteger(length)).toBe(true)
      expect(row).toBeGreaterThanOrEqual(0)
      expect(columnStart).toBeGreaterThanOrEqual(0)
      expect(length).toBeGreaterThanOrEqual(1)
      expect(columnStart + length).toBeLessThanOrEqual(GEO_LAND_GRID)
      const previous = lastColumn.get(row)
      if (previous !== undefined) expect(columnStart).toBeGreaterThan(previous)
      lastColumn.set(row, columnStart + length - 1)
    }
  })

  it("covers a plausible share of the projected world", () => {
    // 墨卡托把高纬度陆地放大，因此占比高于等经纬度投影的约 29%。
    const land = GEO_LAND_RUNS.reduce((total, [, , length]) => total + length, 0)
    const share = land / (GEO_LAND_GRID * GEO_LAND_GRID)
    expect(share).toBeGreaterThan(0.25)
    expect(share).toBeLessThan(0.5)
  })

  it("renders a cached svg path in world units", () => {
    const path = geoLandPath()
    expect(path.startsWith("M")).toBe(true)
    expect(path).toContain("Z")
    expect(path).not.toContain("NaN")
    expect(path).not.toContain("Infinity")
    expect(geoLandPath()).toBe(path)
    const coordinates = [...path.matchAll(/(?:M|h|v)(-?\d+(?:\.\d+)?)/g)].map((match) => Number(match[1]))
    expect(coordinates.length).toBeGreaterThan(0)
    expect(coordinates.every((value) => Number.isFinite(value) && Math.abs(value) <= 360)).toBe(true)
  })

  it("documents its public-domain source", () => {
    expect(GEO_LAND_SOURCE).toContain("Natural Earth")
    expect(GEO_LAND_SOURCE).toContain("public domain")
  })
})
