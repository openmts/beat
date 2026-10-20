import { definitionCandidate, importDescription, importQueryDefaults, importRange, importRefresh, importTags, importTitle, importVariables } from "@/lib/workspace-import-core"
import { importDashboardLinks, importHideLinks } from "@/lib/workspace-import-panel-extras"
import { importPanels } from "@/lib/workspace-import-panels"
import type { DashboardDefinition } from "@/types/metric-workspace"

export const dashboardBundleKind = "beat.dashboard", dashboardBundleVersion = 1

export function exportDashboardJSON(definition: DashboardDefinition): string {
  return JSON.stringify({ kind: dashboardBundleKind, version: dashboardBundleVersion, exported_at: new Date().toISOString(), definition }, null, 2)
}

/** 导入只做结构收敛，语义仍由服务端 ValidateDashboard 裁决。 */
export function importDashboardJSON(raw: string): DashboardDefinition {
  let parsed: unknown = null
  try { parsed = JSON.parse(raw) } catch { parsed = null }
  const candidate = definitionCandidate(parsed)
  if (!candidate) throw new Error("workspace.import_invalid")
  const { source } = candidate, variables = importVariables(candidate.variables), tags = importTags(source.tags)
  const links = importDashboardLinks(source.links)
  return { version: 1, title: importTitle(source.title), description: importDescription(source.description),
    time_range_seconds: importRange(source.time_range_seconds), refresh_seconds: importRefresh(source.refresh_seconds),
    ...importQueryDefaults(source), ...(tags.length ? { tags } : {}), ...(links.length ? { links } : {}),
    ...importHideLinks(source.hide_links), variables, panels: importPanels(candidate.panels, variables) }
}

export function workspaceShareLink(base: { origin: string; pathname: string }, dashboardId: string, nodeId?: string): string {
  const parameters = new URLSearchParams()
  if (dashboardId && dashboardId !== "new") parameters.set("dashboard", dashboardId)
  if (nodeId) parameters.set("node_id", nodeId)
  const query = parameters.toString()
  return `${base.origin}${base.pathname}${query ? `?${query}` : ""}`
}
