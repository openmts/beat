import { YAxis } from "recharts"
import { axisDomain } from "@/lib/workspace-axis"
import { axisScale } from "@/lib/workspace-axis-scale"
import { axisOverrideWidth, centeredZeroDomain } from "@/lib/workspace-axis-display"
import type { AxisOverrideOptions } from "@/lib/workspace-overrides"
import { formatWorkspaceValue, workspaceScale } from "@/lib/workspace-format"
import type { AxisSide } from "@/lib/workspace-axis-sides"
import type { DashboardPanel } from "@/types/metric-workspace"

/**
 * WorkspaceValueAxis 是折线与柱状面板共用的值轴：一侧一根，单位、刻度与轴标签都由调用方给出。
 * 面板声明了固定或柔性上下限时两侧共用同一套限值（与 Grafana 的面板级 Axis 设置一致），
 * 否则每根轴各自按绑定到该轴的序列自动定界，让量级不同的序列分开可读。
 * options 是 Overrides → Axis 解析出来的逐序列轴选项（标题、柔性上下限与轴宽），优先于面板级设置。
 */
export function WorkspaceValueAxis(props: {
  side: AxisSide; unit: string; maximum: number; labelled: boolean; panel: DashboardPanel; locale: string
  options?: AxisOverrideOptions
}) {
  const scale = workspaceScale(props.unit, props.maximum)
  // 逐序列轴标题（Overrides → Axis → Label）永远显示在自己那根轴上；面板级标题只出现在 labelled 的那一侧。
  const label = props.options?.label ?? (props.labelled ? props.panel.axis_label : undefined)
  const fields = { ...props.panel, soft_min: props.options?.softMin ?? props.panel.soft_min, soft_max: props.options?.softMax ?? props.panel.soft_max }
  return <YAxis yAxisId={props.side} orientation={props.side} width={axisOverrideWidth(props.options?.width, props.unit, Boolean(label))}
    tickLine={false} axisLine={false} fontSize={11} scale={axisScale(props.panel)}
    domain={centeredZeroDomain(fields, props.maximum, axisDomain(fields, props.unit))}
    label={label ? { value: label, angle: -90, position: "insideLeft", style: { fontSize: 11 } } : undefined}
    tickFormatter={(value) => formatWorkspaceValue(Number(value), props.unit, { locale: props.locale, decimals: props.panel.decimals, scale })} />
}
