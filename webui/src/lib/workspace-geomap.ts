import { latestWorkspaceValue, workspaceThresholdColor } from "@/lib/workspace-chart"
import { overrideColor, workspaceSeriesColor, workspaceSeriesUnit } from "@/lib/workspace-overrides"
import type { DashboardGeoLocation, DashboardPanel, ExpandedSeries, GeoLayer, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

/**
 * geo map 的选项子集：编辑器、校验与导入导出只碰这几个字段。
 * type 故意保留 string：导入导出拿到的类型在收敛前还是原始取值。
 */
export interface GeoMapOptions {
  type: string
  geo_locations?: DashboardGeoLocation[]
  geo_layer?: GeoLayer
  geo_tile_url?: string
  geo_zoom?: number
  geo_center_latitude?: number
  geo_center_longitude?: number
  geo_marker_size?: number
}

/** 渲染 geo map 所需的面板子集：标记取色还要读该序列的颜色与面板阈值。 */
export interface GeoMapPanel extends GeoMapOptions, Pick<DashboardPanel, "series" | "thresholds" | "overrides"> {}

/** 与服务端 internal/model/dashboard_geomap.go 同规则：地点表 1..64 条、缩放 0..8、标记 2..64。 */
export const geoLayers: GeoLayer[] = ["markers", "heatmap"]
export const minGeoZoom = 0, maxGeoZoom = 8, defaultGeoZoom = 0
export const minGeoMarkerSize = 2, maxGeoMarkerSize = 64, defaultGeoMarkerSize = 8
export const maxGeoLocations = 64
export const maxGeoMatchBytes = 128, maxGeoLabelBytes = 64, maxGeoTileURLBytes = 512
/** Web Mercator 能表示的纬度上限；超出即无法投影，与服务端的 DashboardGeoMaxLatitude 一致。 */
export const geoMaxLatitude = 85.05112878
/** 世界方块边长：缩放 0 的视口正好覆盖整个方块。 */
export const geoWorldSize = 360
/** 标记半径按 600 像素宽的面板折算，保证不同缩放级别下屏幕上的大小一致。 */
export const geoReferenceWidthPx = 600
/** 热力网格的划分数：列数等于行数。 */
export const geoHeatDivisions = 24
/** 热力格最小不透明度：只有一格命中时也要看得见。 */
export const geoHeatMinOpacity = 0.2
/** 图例最多列出的地点数：再多的标记靠地图本身传达。 */
export const geoLegendLimit = 6
/** 底图模板最多渲染的瓦片数：视口很小时避免意外的请求风暴。 */
export const geoMaxTiles = 64

export interface GeoViewport { x: number; y: number; size: number }
export interface GeoMarker { key: string; label: string; x: number; y: number; value: number | null; unit: string; color: string }
export interface GeoHeatCell { column: number; row: number; count: number; ratio: number; value: number | null }
export interface GeoTile { key: string; url: string; x: number; y: number; size: number }

export function geoLayer(panel: GeoMapOptions): GeoLayer {
  return panel.geo_layer && geoLayers.includes(panel.geo_layer) ? panel.geo_layer : "markers"
}

export function geoZoom(panel: GeoMapOptions): number {
  const zoom = panel.geo_zoom
  return typeof zoom === "number" && Number.isInteger(zoom) && zoom >= minGeoZoom && zoom <= maxGeoZoom ? zoom : defaultGeoZoom
}

export function geoMarkerSize(panel: GeoMapOptions): number {
  const size = panel.geo_marker_size
  return typeof size === "number" && Number.isInteger(size) && size >= minGeoMarkerSize && size <= maxGeoMarkerSize ? size : defaultGeoMarkerSize
}

/** geoProject 把经纬度投影到世界方块；非有限值或超出投影范围时返回 null。 */
export function geoProject(latitude: number, longitude: number): { x: number; y: number } | null {
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) return null
  if (Math.abs(latitude) > geoMaxLatitude || Math.abs(longitude) > 180) return null
  const x = longitude + geoWorldSize / 2
  // 纬度先转到墨卡托 y，再归一到 0..1；赤道落在方块中线。
  const y = (1 - Math.asinh(Math.tan((latitude * Math.PI) / 180)) / Math.PI) / 2 * geoWorldSize
  return { x, y }
}

/** geoViewport 取以中心为锚点的正方形窗口，并夹在世界方块内。 */
export function geoViewport(panel: GeoMapOptions): GeoViewport {
  const size = geoWorldSize / 2 ** geoZoom(panel)
  const center = geoProject(clamp(panel.geo_center_latitude ?? 0, -geoMaxLatitude, geoMaxLatitude),
    clamp(panel.geo_center_longitude ?? 0, -180, 180)) ?? { x: size / 2, y: size / 2 }
  return { x: clamp(center.x - size / 2, 0, geoWorldSize - size), y: clamp(center.y - size / 2, 0, geoWorldSize - size), size }
}

/** geoMarkerRadius 把标记直径换算成世界单位，使屏幕上直径接近配置值。 */
export function geoMarkerRadius(panel: GeoMapOptions): number {
  return geoMarkerSize(panel) / 2 * (geoViewport(panel).size / geoReferenceWidthPx)
}

/** geoMarkers 把每条序列落到它的地点上：取最新有效样本，并把阈值或序列颜色写进标记。 */
export function geoMarkers(panel: GeoMapPanel, result: MetricQueryResult, expanded?: ExpandedSeries[]): GeoMarker[] {
  const locations = panel.geo_locations ?? []
  if (!locations.length) return []
  const markers: GeoMarker[] = []
  for (const series of result.series) {
    const location = matchGeoLocation(locations, series, expanded)
    const point = location ? geoProject(location.latitude, location.longitude) : null
    if (!location || !point) continue
    const value = latestWorkspaceValue(series)
    markers.push({ key: series.ref_id, label: location.label || location.match, x: point.x, y: point.y,
      value, unit: workspaceSeriesUnit(panel, series), color: markerColor(panel, series, value, expanded) })
  }
  return markers
}

/** geoUnmatchedSeries 统计查询结果里没有地点（或地点坐标不合法）的序列条数。 */
export function geoUnmatchedSeries(panel: GeoMapPanel, result: MetricQueryResult, expanded?: ExpandedSeries[]): number {
  const locations = panel.geo_locations ?? []
  return result.series.filter((series) => {
    const location = matchGeoLocation(locations, series, expanded)
    return !location || !geoProject(location.latitude, location.longitude)
  }).length
}

/** geoHeatCells 把标记按视口等分成网格计数；ratio 是相对峰值强度，value 是格内最大值。 */
export function geoHeatCells(panel: GeoMapPanel, markers: GeoMarker[]): GeoHeatCell[] {
  const viewport = geoViewport(panel)
  const cells = new Map<number, GeoHeatCell>()
  for (const marker of markers) {
    const column = Math.floor((marker.x - viewport.x) / viewport.size * geoHeatDivisions)
    const row = Math.floor((marker.y - viewport.y) / viewport.size * geoHeatDivisions)
    if (column < 0 || column >= geoHeatDivisions || row < 0 || row >= geoHeatDivisions) continue
    const index = row * geoHeatDivisions + column
    const cell = cells.get(index) ?? { column, row, count: 0, ratio: 0, value: null }
    cell.count += 1
    if (marker.value !== null && (cell.value === null || marker.value > cell.value)) cell.value = marker.value
    cells.set(index, cell)
  }
  const peak = Math.max(0, ...[...cells.values()].map((cell) => cell.count))
  for (const cell of cells.values()) cell.ratio = peak ? cell.count / peak : 0
  return [...cells.values()].sort((left, right) => left.row - right.row || left.column - right.column)
}

/** geoTiles 计算视口内的 XYZ 底图瓦片；未配置模板或瓦片过多时返回空列表。 */
export function geoTiles(panel: GeoMapOptions): GeoTile[] {
  const template = panel.geo_tile_url
  if (!template || !validGeoTileURL(template)) return []
  const zoom = geoZoom(panel), viewport = geoViewport(panel), size = geoWorldSize / 2 ** zoom
  const tiles: GeoTile[] = []
  const first = { column: Math.floor(viewport.x / size), row: Math.floor(viewport.y / size) }
  const last = { column: Math.ceil((viewport.x + viewport.size) / size) - 1, row: Math.ceil((viewport.y + viewport.size) / size) - 1 }
  for (let row = first.row; row <= last.row; row += 1) {
    for (let column = first.column; column <= last.column; column += 1) {
      if (tiles.length >= geoMaxTiles) return tiles
      tiles.push({ key: `${zoom}/${column}/${row}`, url: tileURL(template, zoom, column, row), x: column * size, y: row * size, size })
    }
  }
  return tiles
}

/** geoError 返回目录键，null 表示可以保存；规则与服务端校验一致。 */
export function geoError(panel: GeoMapOptions): string | null {
  if (panel.type !== "geomap") return geoCarriesOptions(panel) ? "workspace.geomap_only" : null
  const locations = panel.geo_locations ?? []
  if (!locations.length) return "workspace.geomap_locations_required"
  if (locations.length > maxGeoLocations) return "workspace.geomap_locations_limit"
  if (locations.some((location) => !validGeoLocation(location)) || duplicateGeoMatches(locations)) return "workspace.geomap_location_invalid"
  if (panel.geo_layer !== undefined && !geoLayers.includes(panel.geo_layer)) return "workspace.geomap_layer_invalid"
  if (panel.geo_zoom !== undefined && !(Number.isInteger(panel.geo_zoom) && panel.geo_zoom >= minGeoZoom && panel.geo_zoom <= maxGeoZoom)) return "workspace.geomap_zoom_invalid"
  if (panel.geo_marker_size !== undefined && !(Number.isInteger(panel.geo_marker_size) && panel.geo_marker_size >= minGeoMarkerSize && panel.geo_marker_size <= maxGeoMarkerSize)) return "workspace.geomap_marker_invalid"
  if ((panel.geo_center_latitude === undefined) !== (panel.geo_center_longitude === undefined)) return "workspace.geomap_center_invalid"
  if (panel.geo_center_latitude !== undefined && !validGeoCenter(panel.geo_center_latitude, panel.geo_center_longitude ?? Number.NaN)) return "workspace.geomap_center_invalid"
  if (!validGeoTileURL(panel.geo_tile_url)) return "workspace.geomap_tile_invalid"
  return null
}

/** geoPatch 切换面板类型时清理字段：只有 geo map 接受这七项。 */
export function geoPatch(type: string): Pick<DashboardPanel, "geo_locations" | "geo_layer" | "geo_tile_url" | "geo_zoom" |
  "geo_center_latitude" | "geo_center_longitude" | "geo_marker_size"> {
  if (type === "geomap") return {}
  return { geo_locations: undefined, geo_layer: undefined, geo_tile_url: undefined, geo_zoom: undefined,
    geo_center_latitude: undefined, geo_center_longitude: undefined, geo_marker_size: undefined }
}

/** emptyGeoLocation 返回一条空地点：编辑者只需要填匹配串与经纬度，缺项由校验拦住。 */
export function emptyGeoLocation(): DashboardGeoLocation {
  return { match: "", latitude: 0, longitude: 0 }
}

/** validGeoTileURL 只接受带 {z}/{x}/{y} 占位符的 http(s) 模板；空值表示使用内置底图。 */
export function validGeoTileURL(raw?: string): boolean {
  if (!raw) return true
  if (utf8Length(raw) > maxGeoTileURLBytes) return false
  if (!raw.startsWith("https://") && !raw.startsWith("http://")) return false
  if (/\s/.test(raw) || [...raw].some((character) => (character.codePointAt(0) ?? 0) < 32)) return false
  const rest = raw.replaceAll("{z}", "").replaceAll("{x}", "").replaceAll("{y}", "")
  if (rest.includes("{") || rest.includes("}")) return false
  return ["{z}", "{x}", "{y}"].every((token) => raw.includes(token))
}

function geoCarriesOptions(panel: GeoMapOptions): boolean {
  return (panel.geo_locations?.length ?? 0) > 0 || Boolean(panel.geo_layer) || Boolean(panel.geo_tile_url) ||
    panel.geo_zoom !== undefined || panel.geo_center_latitude !== undefined || panel.geo_center_longitude !== undefined ||
    panel.geo_marker_size !== undefined
}

function validGeoLocation(location: DashboardGeoLocation): boolean {
  if (typeof location.match !== "string" || !location.match.trim() || utf8Length(location.match) > maxGeoMatchBytes) return false
  if (location.label !== undefined && location.label !== "" && utf8Length(location.label) > maxGeoLabelBytes) return false
  return validGeoCenter(location.latitude, location.longitude)
}

function validGeoCenter(latitude: number, longitude: number): boolean {
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) return false
  return Math.abs(latitude) <= geoMaxLatitude && Math.abs(longitude) <= 180
}

function duplicateGeoMatches(locations: DashboardGeoLocation[]): boolean {
  const seen = new Set<string>()
  for (const location of locations) {
    const key = location.match.trim().toLowerCase()
    if (seen.has(key)) return true
    seen.add(key)
  }
  return false
}

/** matchGeoLocation 依次按节点 ID、设备 ID、节点名匹配地点表的匹配串，忽略大小写与首尾空白。 */
function matchGeoLocation(locations: DashboardGeoLocation[], series: MetricQueryResult["series"][number], expanded?: ExpandedSeries[]): DashboardGeoLocation | null {
  const nodeName = expanded?.find((entry) => entry.ref_id === series.ref_id)?.node_name
  const keys = [series.node_id, series.device_id, nodeName]
    .filter((value): value is string => Boolean(value))
    .map((value) => value.trim().toLowerCase())
  return locations.find((location) => keys.includes(location.match.trim().toLowerCase())) ?? null
}

/** markerColor 与 stat/gauge 一致：覆盖优先，其次阈值，最后用该序列自身的颜色。 */
function markerColor(panel: GeoMapPanel, series: MetricResultSeries, value: number | null, expanded?: ExpandedSeries[]): string {
  const overridden = overrideColor(panel, series)
  if (overridden) return overridden
  if (panel.thresholds.length) return workspaceThresholdColor(panel as unknown as DashboardPanel, value)
  return workspaceSeriesColor(panel, series, expanded)
}

function tileURL(template: string, zoom: number, column: number, row: number): string {
  return template.replaceAll("{z}", String(zoom)).replaceAll("{x}", String(column)).replaceAll("{y}", String(row))
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(Math.max(value, minimum), maximum)
}

function utf8Length(value: string): number {
  return new TextEncoder().encode(value).length
}
