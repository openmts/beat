import type { WorkspacePage } from "@/hooks/use-workspace-page"
import { getPublicDashboard, type WorkspaceSources } from "@/lib/metric-workspace-api"
import { moveWorkspacePanel, replaceWorkspacePanel } from "@/lib/workspace-editor"
import { newWorkspacePanel } from "@/lib/workspace-panel-editor"
import { downloadWorkspaceFile } from "@/lib/workspace-export"
import { exportDashboardJSON, importDashboardJSON, workspaceShareLink } from "@/lib/workspace-portable"
import { workspaceAllSelection } from "@/lib/workspace-variable-options"
import { workspaceVariableParameters } from "@/lib/workspace-url-state"
import { randomUUID } from "@/lib/random-id"
import type { DashboardPanel } from "@/types/metric-workspace"
import type { WorkspacePanelAction } from "@/components/metric-workspace/panel"
import type { WorkspaceToolbarAction } from "@/components/metric-workspace/toolbar"

export interface WorkspaceActionContext { page: WorkspacePage; sources: WorkspaceSources; t: (key: string) => string }

export function workspaceToolbarAction(action: WorkspaceToolbarAction, context: WorkspaceActionContext): void {
  const { page, t } = context
  const editor = page.editor
  const actions: Record<WorkspaceToolbarAction, () => void> = {
    new: () => afterWorkspaceDiscard(page, () => editor.create(t("workspace.untitled"))),
    save: () => editor.record.scope === "personal" ? void editor.save() : withWorkspaceIdentity(page, () => editor.save()),
    copy: () => { void editor.save(true) }, details: () => page.setDetails(true),
    discard: () => page.setConfirmation({ kind: "discard", action: editor.discard }),
    reload: () => afterWorkspaceDiscard(page, () => { void editor.reload() }),
    delete: () => page.setConfirmation({ kind: "delete", action: () => { void editor.remove() } }),
    rollback: () => page.setConfirmation({ kind: "rollback", action: () => editor.record.scope === "personal" ? void editor.rollback() : withWorkspaceIdentity(page, editor.rollback) }),
    publish: () => publishWorkspaceDefault(context), publish_node: () => { void publishWorkspaceNode(context) },
    export: () => exportWorkspaceDashboard(context), share: () => shareWorkspaceDashboard(context),
    snapshot: () => page.setSnapshots(true), versions: () => page.setVersions(true), panel_library: () => page.setLibraryOpen(true),
    duplicate: () => page.setDuplicateOpen(true), paste_panel: () => page.setPastePanel(true),
  }
  actions[action]()
}

function exportWorkspaceDashboard(context: WorkspaceActionContext) {
  const { page, t } = context
  try { downloadWorkspaceFile(page.editor.record.definition.title, exportDashboardJSON(page.editor.record.definition), "json") }
  catch { page.setError(t("workspace.export_failed")) }
}

function shareWorkspaceDashboard(context: WorkspaceActionContext) {
  const { page } = context
  const definition = page.editor.record.definition
  const variable = definition.variables.find((value) => value.type === "node")
  const selected = variable ? page.values[variable.name]?.[0] : undefined
  const link = new URL(workspaceShareLink(window.location, page.editor.record.id, selected === workspaceAllSelection ? undefined : selected))
  // 分享链接带上全部变量选择，收件人打开就是你当前看到的那块看板。
  for (const [key, value] of workspaceVariableParameters(definition.variables, page.values)) link.searchParams.set(key, value)
  page.setLink(link.toString())
}

/** 导入只落到未保存草稿，仍需服务端校验并显式保存。 */
export async function importWorkspaceFile(page: WorkspacePage, file: File): Promise<void> {
  try { page.editor.adopt(importDashboardJSON(await file.text())) }
  catch (error) { page.setError(error instanceof Error ? error.message : "workspace.import_invalid") }
}

export function afterWorkspaceDiscard(page: WorkspacePage, action: () => void) {
  if (page.editor.dirty) page.setConfirmation({ kind: "discard", action })
  else action()
}

function withWorkspaceIdentity(page: WorkspacePage, action: () => Promise<void>) {
  if (page.proofUntil && Date.parse(page.proofUntil) > Date.now()) { void action(); return }
  page.setReauth({ action })
}

function publishWorkspaceDefault(context: WorkspaceActionContext) {
  const target = context.page.editor.dashboards.find((value) => value.id === "public-default")
  if (target) withWorkspaceIdentity(context.page, () => context.page.editor.publish(target))
}

async function publishWorkspaceNode(context: WorkspaceActionContext) {
  const { page } = context
  const variable = page.editor.record.definition.variables.find((value) => value.type === "node")
  const selected = variable ? page.values[variable.name]?.[0] ?? "" : ""
  const node = context.sources.nodes.find((value) => value.id === (selected === workspaceAllSelection ? "" : selected))
  if (!node || !node.is_public) { page.setError("workspace.select_public_node"); return }
  try {
    const current = await getPublicDashboard({ nodeId: node.id, signal: new AbortController().signal })
    const target = { ...current, id: `node-${node.id}`, node_id: node.id, scope: "node" as const, revision: current.id === `node-${node.id}` ? current.revision : 0 }
    withWorkspaceIdentity(page, () => page.editor.publish(target))
  } catch (error) { page.setError(error instanceof Error ? error.message : "workspace.save_failed") }
}

/** 恢复历史版本；共享看板沿用发布/回滚的最近认证要求，个人看板直接恢复。 */
export function restoreWorkspaceVersion(page: WorkspacePage, revision: number, summary: string): void {
  const restore = () => page.editor.restoreVersion(revision, summary)
  if (page.editor.record.scope === "personal") void restore()
  else withWorkspaceIdentity(page, restore)
}

export function workspacePanelAction(context: WorkspaceActionContext, panel: DashboardPanel, action: WorkspacePanelAction) {
  const { page } = context
  if (action === "edit") { page.setPanel(panel); return }
  if (action === "link_alerts") { page.setAlertPanel(panel); return }
  if (action === "save_library") { page.setLibraryPanel(panel); return }
  if (action === "duplicate") { page.setPanel({ ...structuredClone(panel), id: randomUUID() }); return }
  if (action === "remove") { page.editor.update((definition) => ({ ...definition, panels: definition.panels.filter((value) => value.id !== panel.id) })); return }
  if (action === "up" || action === "down") {
    page.editor.update((definition) => moveWorkspacePanel(definition, panel.id, definition.panels.findIndex((value) => value.id === panel.id) + (action === "up" ? -1 : 1)))
    return
  }
  page.editor.update((definition) => replaceWorkspacePanel(definition, { ...panel, size: action }))
}

/** 面板追加到末尾；超过 24 块时 replaceWorkspacePanel 抛出 workspace.panel_limit 并显示为错误。 */
export function appendPastedWorkspacePanel(page: WorkspacePage, panel: DashboardPanel): void {
  try { page.editor.update((definition) => replaceWorkspacePanel(definition, panel)) }
  catch (error) { page.setError(error instanceof Error ? error.message : "workspace.invalid_panel") }
}

export function addWorkspacePanel(context: WorkspaceActionContext) {
  context.page.setPanel(newWorkspacePanel(context.sources.catalog.metrics, context.page.editor.record.definition))
}
