import { gradientFill } from "@/lib/workspace-gradient"
import { sparklineAreaPaths, sparklineHeight, sparklineSegments, sparklineStyle, sparklineWidth } from "@/lib/workspace-sparkline"
import { WorkspaceGradientDefs } from "./gradient-fill"
import type { DashboardPanel, MetricResultSeries } from "@/types/metric-workspace"

/** Stat 面板的迷你趋势图：与 Grafana 一致，只在面板打开 Sparkline 时渲染，配色跟随阈值取值。
 *  线宽、填充与渐变来自面板配置，缺省是 2 px 描边与纯折线；渐变复用折线面板同一套停靠点与 SVG id。
 *  图形本身是装饰，数值与取样时间已经可读，因此整块对读屏器隐藏。 */
export function WorkspaceSparkline(props: { panel: DashboardPanel; series: MetricResultSeries; color: string }) {
  const segments = sparklineSegments(props.series.points)
  if (segments.length === 0) return null
  const style = sparklineStyle(props.panel)
  const gradient = style.gradientMode
  const areas = style.fillOpacity > 0 ? sparklineAreaPaths(props.series.points) : []
  const fill = gradientFill(props.panel.id, props.series.ref_id, gradient, props.color)
  return <svg data-workspace-sparkline viewBox={`0 0 ${sparklineWidth} ${sparklineHeight}`} preserveAspectRatio="none" aria-hidden="true" className="h-8 w-full">
    {gradient !== "none" && <WorkspaceGradientDefs panelId={props.panel.id} mode={gradient} opacity={style.fillOpacity / 100}
      direction="down" series={[{ ref_id: props.series.ref_id, color: props.color }]} />}
    {areas.map((path, index) => <path key={`area-${index}`} data-workspace-sparkline-area d={path} fill={fill}
      fillOpacity={gradient === "none" ? style.fillOpacity / 100 : 1} stroke="none" />)}
    {segments.map((path, index) => <path key={index} d={path} fill="none" stroke={props.color} strokeWidth={style.lineWidth} strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />)}
  </svg>
}
