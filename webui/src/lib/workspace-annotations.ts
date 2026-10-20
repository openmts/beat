import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import { panelTimeShiftSeconds, shiftQueryDomain } from "@/lib/workspace-time-override"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 落在时间轴上的注释标记：窗口已裁剪到当前查询范围。 */
export interface WorkspaceAnnotationMarker {
  annotation: DashboardAnnotation
  start: number
  end: number
}

/**
 * 选出一条面板时间轴上要绘制的注释。
 *
 * 看板级注释（`panel_id` 为空）出现在所有面板上；面板级注释只出现在自己的面板上。
 * 窗口与查询范围不相交、或者时间戳无法解析的注释一律跳过，裁剪后的窗口用于绘制色带。
 */
export function workspaceAnnotationMarkers(
  values: DashboardAnnotation[], panelId: string, from: number, to: number,
): WorkspaceAnnotationMarker[] {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) return []
  return values
    .map((annotation) => ({ annotation, start: Date.parse(annotation.from), end: Date.parse(annotation.to) }))
    .filter((item) => Number.isFinite(item.start) && Number.isFinite(item.end)
      && item.end >= from && item.start <= to
      && (!item.annotation.panel_id || item.annotation.panel_id === panelId))
    .map((item) => ({ annotation: item.annotation, start: Math.max(item.start, from), end: Math.min(item.end, to) }))
    .sort((left, right) => left.start - right.start)
}

/** 面板级注释在标签列表里显示所属面板标题；看板级注释留空。 */
export function workspaceAnnotationScopeLabel(
  value: DashboardAnnotation, panelTitles: Record<string, string>,
): string | null {
  if (!value.panel_id) return null
  return panelTitles[value.panel_id] ?? value.panel_id
}

/**
 * 注释拉取窗口：按看板内最大的面板时间平移向前扩展。
 * 平移面板查询的是更早的窗口，若仍按看板范围拉取，这些面板的时间轴标记会缺数据。
 */
export function annotationFetchWindow(domain: { from: string; to: string }, panels: DashboardPanel[]): { from: string; to: string } {
  const shift = Math.max(0, ...panels.map((panel) => panelTimeShiftSeconds(panel) ?? 0))
  return shift === 0 ? domain : shiftQueryDomain(domain, shift)
}
