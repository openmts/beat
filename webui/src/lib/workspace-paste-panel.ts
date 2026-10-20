import { validateWorkspacePanel } from "@/lib/workspace-panel-editor"
import { randomUUID } from "@/lib/random-id"
import type { DashboardDefinition, DashboardPanel, MetricDefinition } from "@/types/metric-workspace"

/** 单面板 JSON 远小于整份看板导出，64 KiB 只为拦住误粘的整库文件。 */
export const maximumPastedPanelBytes = 64 * 1024
export const maximumWorkspacePanels = 24
const panelIdPattern = /^[a-zA-Z0-9_-]{1,64}$/

export type PastedWorkspacePanel = { panel: DashboardPanel } | { error: string }

/** 粘贴的面板 JSON 走与编辑器同一条校验：先归一成 DashboardPanel，再交给共享 validator。 */
export function parsePastedPanelJSON(text: string, definition: DashboardDefinition, metrics: MetricDefinition[]): PastedWorkspacePanel {
  const trimmed = text.trim()
  if (!trimmed) return { error: "workspace.paste_empty" }
  if (new TextEncoder().encode(trimmed).length > maximumPastedPanelBytes) return { error: "workspace.paste_too_large" }
  if (definition.panels.length >= maximumWorkspacePanels) return { error: "workspace.panel_limit" }
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch (_) {
    void _
    return { error: "workspace.paste_invalid" }
  }
  const panel = pastedPanel(parsed, definition)
  if (!panel) return { error: "workspace.paste_invalid" }
  const failure = validateWorkspacePanel(panel, metrics, definition.variables)
  return failure ? { error: failure } : { panel }
}

/** 缺省字段按服务端默认值补齐；占位或冲突的面板 id 换成新的 UUID。 */
function pastedPanel(parsed: unknown, definition: DashboardDefinition): DashboardPanel | null {
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return null
  const source = parsed as Record<string, unknown>
  if (typeof source.type !== "string") return null
  const free = typeof source.id === "string" && panelIdPattern.test(source.id) && !definition.panels.some((value) => value.id === source.id)
  return { ...source, id: free ? source.id : randomUUID(), title: typeof source.title === "string" ? source.title : "",
    series: Array.isArray(source.series) ? source.series : [], thresholds: Array.isArray(source.thresholds) ? source.thresholds : [] } as DashboardPanel
}
