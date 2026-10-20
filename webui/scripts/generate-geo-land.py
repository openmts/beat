#!/usr/bin/env python3
"""生成指标工作区 Geo map 面板使用的世界陆地底图数据。

数据来源：Natural Earth 110m 陆地多边形（public domain）：
https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_land.geojson

坐标系：Web Mercator 世界方块，与 slippy map tiles 对齐。
    x_norm = (lon + 180) / 360
    y_norm = (1 - asinh(tan(lat)) / pi) / 2     # lat 先转弧度，北→南

栅格化用扫描线法：对每一行取行中心纬度，求所有多边形边与该纬线的交点，
按奇偶规则标记落在陆地内的列，避免逐格做点在多边形内判断。

重新生成：
    python3 webui/scripts/generate-geo-land.py
产物变化后必须重跑 `webui/src/lib/workspace-geo-land.test.ts`。
"""

import json
import math
import pathlib
import urllib.request

SOURCE_URL = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_land.geojson"
CACHE = pathlib.Path("/tmp/ne_110m_land.geojson")
GRID_CANDIDATES = (128, 112, 96)
MAX_BYTES = 34 * 1024
OUTPUT = pathlib.Path(__file__).resolve().parents[1] / "src" / "lib" / "workspace-geo-land.ts"


def load_geojson() -> dict[str, object]:
    if not CACHE.exists():
        with urllib.request.urlopen(SOURCE_URL, timeout=60) as response:
            CACHE.write_bytes(response.read())
    return json.loads(CACHE.read_text(encoding="utf-8"))


def mercator(lon: float, lat: float) -> tuple[float, float]:
    clamped = max(min(lat, 85.05112878), -85.05112878)
    x = (lon + 180.0) / 360.0
    y = (1.0 - math.asinh(math.tan(math.radians(clamped))) / math.pi) / 2.0
    return x, y


def rings(geojson: dict[str, object]) -> list[list[tuple[float, float]]]:
    result: list[list[tuple[float, float]]] = []
    for feature in geojson.get("features", []):
        geometry = feature.get("geometry", {})
        polygons = geometry.get("coordinates", [])
        if geometry.get("type") == "Polygon":
            polygons = [polygons]
        for polygon in polygons:
            for ring in polygon:
                result.append([mercator(point[0], point[1]) for point in ring])
    return result


def row_coverage(polygon_rings: list[list[tuple[float, float]]], y: float, columns: int) -> list[bool]:
    crossings: list[float] = []
    for ring in polygon_rings:
        for index in range(len(ring) - 1):
            x1, y1 = ring[index]
            x2, y2 = ring[index + 1]
            if (y1 > y) == (y2 > y):
                continue
            crossings.append(x1 + (y - y1) / (y2 - y1) * (x2 - x1))
    crossings.sort()
    covered = [False] * columns
    for index in range(0, len(crossings) - 1, 2):
        start = math.ceil(crossings[index] * columns - 0.5)
        end = math.floor(crossings[index + 1] * columns - 0.5)
        for column in range(max(start, 0), min(end, columns - 1) + 1):
            covered[column] = True
    return covered


def rasterize(polygon_rings: list[list[tuple[float, float]]], grid: int) -> list[tuple[int, int, int]]:
    runs: list[tuple[int, int, int]] = []
    for row in range(grid):
        covered = row_coverage(polygon_rings, (row + 0.5) / grid, grid)
        column = 0
        while column < grid:
            if not covered[column]:
                column += 1
                continue
            start = column
            while column < grid and covered[column]:
                column += 1
            runs.append((row, start, column - start))
    return runs


def render(grid: int, runs: list[tuple[int, int, int]]) -> str:
    land = sum(length for _, _, length in runs)
    share = land / (grid * grid) * 100
    body = ",\n".join(f"  [{row}, {start}, {length}]" for row, start, length in runs)
    return f'''// 由 webui/scripts/generate-geo-land.py 生成，请勿手工编辑。
//
// 数据来源：{SOURCE_URL}
// 许可：Natural Earth 为 public domain（无需署名，本注释保留出处便于追溯）。
// 坐标系：Web Mercator 世界方块，x/y 都归一化到 0..1（x 西→东，y 北→南）；
// 渲染时乘 360 得到世界单位，与 Geo map 的视口 viewBox 同一空间。
// 栅格：{grid}x{grid}（占比越高底图越细，源码体积越大；当前陆地占比约 {share:.1f}%）。
// 栅格化：按行中心纬度做扫描线求交，奇偶规则标记陆地列，再按行做 run-length 编码。
// 重新生成：python3 webui/scripts/generate-geo-land.py（产物变化后必须重跑同目录测试）。

/** 行列数：格子的世界单位边长是 360 / GEO_LAND_GRID。 */
export const GEO_LAND_GRID = {grid}

/** 陆地游程：每项是 [row, columnStart, length]，按 row、columnStart 升序且不重叠。 */
export const GEO_LAND_RUNS: readonly (readonly [number, number, number])[] = [
{body},
]

/** 数据来源与许可，测试与文档共用同一份出处说明。 */
export const GEO_LAND_SOURCE = "Natural Earth 110m land (public domain) via {SOURCE_URL}"

let cachedPath: string | null = null

/** geoLandPath 把陆地游程拼成世界单位 0..360 的 SVG path；结果缓存，重复调用返回同一字符串。 */
export function geoLandPath(): string {{
  if (cachedPath !== null) return cachedPath
  const cell = 360 / GEO_LAND_GRID
  cachedPath = GEO_LAND_RUNS.map(([row, column, length]) => {{
    const x = trim(column * cell), y = trim(row * cell), width = trim(length * cell), height = trim(cell)
    return `M${{x}} ${{y}}h${{width}}v${{height}}h${{-width}}Z`
  }}).join("")
  return cachedPath
}}

/** trim 输出最多两位小数并去掉多余的 .0，避免 path 里出现超长浮点串。 */
function trim(value: number): number {{
  return Math.round(value * 100) / 100
}}
'''


def main() -> None:
    polygon_rings = rings(load_geojson())
    for grid in GRID_CANDIDATES:
        source = render(grid, rasterize(polygon_rings, grid))
        size = len(source.encode("utf-8"))
        print(f"grid {grid}: {size} bytes")
        if size <= MAX_BYTES or grid == GRID_CANDIDATES[-1]:
            OUTPUT.write_text(source, encoding="utf-8")
            OUTPUT.chmod(0o600)
            print(f"wrote {OUTPUT} ({size} bytes)")
            return


if __name__ == "__main__":
    main()
