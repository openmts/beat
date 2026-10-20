import { gradientId, gradientStops } from "@/lib/workspace-gradient"
import type { GradientMode } from "@/lib/workspace-line-style"

/** WorkspaceGradientDefs 为每条可见序列声明一个线性渐变：面积图从折线向下渐隐，
 *  柱状图从底部实色向上渐隐，与 Grafana 的 Gradient mode 观感一致。
 *  序列自带 opacity 时优先用它，这样字段覆盖改过的填充只影响被覆盖的序列。 */
export function WorkspaceGradientDefs(props: {
  panelId: string; mode: GradientMode; opacity: number; direction: "down" | "up"
  series: { ref_id: string; color: string; opacity?: number }[]
}) {
  if (props.mode === "none") return null
  const from = props.direction === "down" ? "0" : "1", to = props.direction === "down" ? "1" : "0"
  return <defs>{props.series.map((series) => <linearGradient key={series.ref_id} id={gradientId(props.panelId, series.ref_id)}
    x1="0" y1={from} x2="0" y2={to}>
    {gradientStops(series.color, props.mode, series.opacity ?? props.opacity).map((stop) =>
      <stop key={stop.offset} offset={`${stop.offset * 100}%`} stopColor={stop.color} stopOpacity={stop.opacity} />)}
  </linearGradient>)}</defs>
}
