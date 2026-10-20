import { softAxisPanelTypes } from "./workspace-axis"
import type { MetricResultSeries, PanelType } from "@/types/metric-workspace"

/** Grafana 的双 Y 轴：每条序列最终落在左轴还是右轴。 */
export type AxisSide = "left" | "right"

export interface AxisSideGroups<T> {
  left: T[]
  right: T[]
}

/** axisSideGroup 按轴位置把序列分成两组；空组表示该侧不画轴。 */
export function axisSideGroup<T>(items: T[], sideOf: (item: T) => AxisSide): AxisSideGroups<T> {
  const groups: AxisSideGroups<T> = { left: [], right: [] }
  for (const item of items) groups[sideOf(item)].push(item)
  return groups
}

/** axisHasBothSides 只有两侧都分到序列时才画第二根轴，否则面板保持今天的一根轴。 */
export function axisHasBothSides(groups: AxisSideGroups<unknown>): boolean {
  return groups.left.length > 0 && groups.right.length > 0
}

/** axisSideMaximum 取一侧序列的最大绝对值，供该轴的刻度与居中零点使用。 */
export function axisSideMaximum(series: MetricResultSeries[]): number {
  return Math.max(0, ...series.flatMap((value) => value.points.map((point) => Math.abs(point.value ?? 0))))
}

/** axisSideSupported 判断面板类型是否消费按序列轴位置：与面板级轴位置一致，只含折线与柱状面板。 */
export function axisSideSupported(type: string | undefined): boolean {
  return type !== undefined && softAxisPanelTypes.includes(type as PanelType)
}
