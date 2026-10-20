// Explore → 看板（Grafana 的 Add to dashboard）：把当前启用的查询行转成时序面板并追加到已有看板。
// 边界与 Grafana 一致：面板继承目标看板的时间范围与变量，Explore 的临时时间范围不写进面板，
// 否则看板会被一次性探索钉死在一个固定窗口上。

import { exploreRunnable, type ExploreQuery, type ExploreState } from "@/lib/metric-explore"
import type { DashboardDefinition, DashboardPanel, DashboardSeries } from "@/types/metric-workspace"

/** 与服务端 model.MaxDashboardPanels 对齐：看板最多 24 块面板。 */
export const maximumExplorePanels = 24
/** 服务端要求单块面板 1..8 条序列，超出的查询行按顺序切成多块。 */
const seriesPerPanel = 8

/** buildExplorePanels 把启用的查询行按 8 条一组切成时序面板；返回的面板还没有分配 ID。 */
export function buildExplorePanels(state: ExploreState, title: string): Omit<DashboardPanel, "id">[] {
  const rows = exploreRunnable(state)
  const panels: Omit<DashboardPanel, "id">[] = []
  for (let start = 0; start < rows.length; start += seriesPerPanel) {
    const chunk = rows.slice(start, start + seriesPerPanel)
    panels.push({
      title: panels.length === 0 ? title : `${title} ${panels.length + 1}`,
      type: "timeseries", size: "large", legend: true, unit: "auto", thresholds: [], series: chunk.map(panelSeries),
    })
  }
  return panels
}

function panelSeries(row: ExploreQuery): DashboardSeries {
  return {
    ref_id: row.ref_id, node_id: row.node_id, metric: row.metric, aggregation: row.aggregation,
    color: row.color, label: "", ...(row.device_id ? { device_id: row.device_id } : {}),
  }
}

/**
 * appendExplorePanels 追加面板并为每块分配未被占用的 `explore-n` ID；
 * 超过面板上限时返回 null，由调用方给出明确提示，而不是悄悄丢面板。
 */
export function appendExplorePanels(definition: DashboardDefinition, drafts: Omit<DashboardPanel, "id">[]): DashboardDefinition | null {
  if (definition.panels.length + drafts.length > maximumExplorePanels) return null
  const used = new Set(definition.panels.map((panel) => panel.id))
  return { ...definition, panels: [...definition.panels, ...drafts.map((draft) => ({ ...draft, id: nextPanelID(used) }))] }
}

function nextPanelID(used: Set<string>): string {
  let index = 1
  while (used.has(`explore-${index}`)) index += 1
  const id = `explore-${index}`
  used.add(id)
  return id
}
