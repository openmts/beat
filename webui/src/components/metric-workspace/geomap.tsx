import { useMemo, type ReactElement } from "react"
import { useLocale } from "@/context/locale"
import { panelColors, workspaceThresholdColor } from "@/lib/workspace-chart"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import { geoHeatCells, geoHeatDivisions, geoHeatMinOpacity, geoLayer, geoLegendLimit, geoMarkerRadius,
  geoMarkers, geoTiles, geoUnmatchedSeries, geoViewport, geoWorldSize } from "@/lib/workspace-geomap"
import { geoLandPath } from "@/lib/workspace-geo-land"
import type { DashboardPanel, ExpandedSeries, MetricQueryResult } from "@/types/metric-workspace"

/** 经纬网间隔：30° 一条，赤道与本初子午线单独加粗。 */
const graticuleStep = 30

/**
 * Grafana 的 Geomap：地点表把序列落到地图上，按标记或热力两种图层渲染。
 * 底图默认是内置的世界陆地轮廓（离线可用）；配置了 XYZ 模板才会向底图服务取瓦片。
 */
export function WorkspaceGeoMap(props: { panel: DashboardPanel; result: MetricQueryResult; expanded?: ExpandedSeries[]; height?: number }) {
  const { locale, t } = useLocale()
  const viewport = geoViewport(props.panel)
  const markers = geoMarkers(props.panel, props.result, props.expanded)
  const unmatched = geoUnmatchedSeries(props.panel, props.result, props.expanded)
  const layer = geoLayer(props.panel)
  const cells = layer === "heatmap" ? geoHeatCells(props.panel, markers) : []
  const tiles = geoTiles(props.panel)
  const radius = geoMarkerRadius(props.panel)
  // 底图路径只与固定栅格有关，跨渲染与跨面板复用同一条缓存字符串。
  const land = useMemo(() => geoLandPath(), [])
  const height = props.height ?? 224
  if (!markers.length) {
    return <div className="grid h-60 place-content-center gap-1 px-6 text-center">
      <p className="text-sm font-medium">{t("workspace.no_data")}</p>
      <p className="text-xs text-muted-foreground">{t("workspace.geomap_no_match")}</p>
    </div>
  }
  const heatColor = props.panel.thresholds.length ? null : panelColors[props.panel.series[0]?.color ?? "blue"]
  return <div className="flex flex-col gap-2 px-4 py-3" style={{ minHeight: height }}>
    <svg role="img" data-geomap={layer} className="w-full rounded border bg-muted/20" style={{ height }}
      viewBox={`${round(viewport.x)} ${round(viewport.y)} ${round(viewport.size)} ${round(viewport.size)}`}
      aria-label={`${props.panel.title} · ${t(layer === "heatmap" ? "workspace.geomap_layer_heatmap" : "workspace.geomap_layer_markers")}`}>
      <path d={land} data-geomap-land className="text-muted-foreground/25" fill="currentColor" />
      <Graticule viewport={viewport} />
      {tiles.map((tile) => <image key={tile.key} data-geomap-tile={tile.key} href={tile.url}
        x={tile.x} y={tile.y} width={tile.size} height={tile.size} preserveAspectRatio="none" />)}
      {layer === "heatmap"
        ? cells.map((cell) => <rect key={`${cell.row}-${cell.column}`} data-geomap-heat={cell.count}
          x={viewport.x + cell.column * (viewport.size / geoHeatDivisions)} y={viewport.y + cell.row * (viewport.size / geoHeatDivisions)}
          width={viewport.size / geoHeatDivisions} height={viewport.size / geoHeatDivisions}
          fill={heatColor ?? workspaceThresholdColor(props.panel, cell.value)}
          fillOpacity={geoHeatMinOpacity + cell.ratio * (1 - geoHeatMinOpacity)} />)
        : markers.map((marker) => <circle key={marker.key} data-geomap-marker={marker.key} cx={marker.x} cy={marker.y}
          r={radius} fill={marker.color} fillOpacity={0.85} stroke="#ffffff" strokeWidth={radius / 4} />)}
    </svg>
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[10px] text-muted-foreground">
      {markers.slice(0, geoLegendLimit).map((marker) => <span key={marker.key} className="flex items-center gap-1">
        <span aria-hidden="true" className="size-2 rounded-full" style={{ backgroundColor: marker.color }} />
        <span>{marker.label}</span>
        <span className="tabular-nums">{formatWorkspaceValue(marker.value, marker.unit, { locale, decimals: props.panel.decimals })}</span>
      </span>)}
      {unmatched > 0 && <span>{t("workspace.geomap_unmatched", { count: unmatched })}</span>}
    </div>
  </div>
}

/** 经纬网：视口内每 30° 一条细线，赤道与本初子午线着色更重，便于判断位置。 */
function Graticule(props: { viewport: { x: number; y: number; size: number } }) {
  const { x, y, size } = props.viewport
  const lines: ReactElement[] = []
  for (let longitude = 0; longitude <= geoWorldSize; longitude += graticuleStep) {
    lines.push(<line key={`lon-${longitude}`} data-geomap-graticule={longitude} x1={longitude} y1={y} x2={longitude} y2={y + size}
      className={longitude === geoWorldSize / 2 ? "stroke-muted-foreground/40" : "stroke-muted-foreground/20"} strokeWidth={size / 720} />)
  }
  for (let latitude = 0; latitude <= geoWorldSize; latitude += graticuleStep) {
    lines.push(<line key={`lat-${latitude}`} data-geomap-graticule={`lat-${latitude}`} x1={x} y1={latitude} x2={x + size} y2={latitude}
      className={latitude === geoWorldSize / 2 ? "stroke-muted-foreground/40" : "stroke-muted-foreground/20"} strokeWidth={size / 720} />)
  }
  const visible = lines.filter((line) => {
    const properties = line.props as { x1: number; y1: number; x2: number; y2: number }
    return properties.x2 >= x && properties.x1 <= x + size && properties.y2 >= y && properties.y1 <= y + size
  })
  return <g aria-hidden="true">{visible}</g>
}

/** 视口坐标保留两位小数，避免 SVG 属性里出现超长浮点串。 */
function round(value: number): number {
  return Math.round(value * 100) / 100
}
