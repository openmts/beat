import type { DashboardDefinition, DashboardPanel, MetricDashboard } from "@/types/metric-workspace"

export interface WorkspaceEditorState {
  record: MetricDashboard
  saved: MetricDashboard
  dashboards: MetricDashboard[]
  busy: boolean
  error?: string
  conflict: boolean
}

export function workspaceDirty(state: WorkspaceEditorState): boolean {
  return state.record.id === "new" || JSON.stringify(state.record.definition) !== JSON.stringify(state.saved.definition)
}

export function applyDashboardSave(state: WorkspaceEditorState, saved: MetricDashboard, captured: MetricDashboard): WorkspaceEditorState {
  const dashboards = state.dashboards.filter((value) => value.id !== saved.id).concat(saved)
  if (state.record.id !== captured.id) return { ...state, dashboards, busy: false }
  const definition = JSON.stringify(state.record.definition) === JSON.stringify(captured.definition) ? saved.definition : state.record.definition
  return { ...state, dashboards, record: { ...saved, definition }, saved, busy: false, error: undefined, conflict: false }
}

export function moveWorkspacePanel(definition: DashboardDefinition, id: string, destination: number): DashboardDefinition {
  const index = definition.panels.findIndex((panel) => panel.id === id)
  if (index < 0 || destination < 0 || destination >= definition.panels.length) return definition
  const panels = [...definition.panels]
  const [panel] = panels.splice(index, 1)
  panels.splice(destination, 0, panel)
  return { ...definition, panels }
}

export function replaceWorkspacePanel(definition: DashboardDefinition, panel: DashboardPanel): DashboardDefinition {
  if (definition.panels.some((value) => value.id === panel.id)) return { ...definition, panels: definition.panels.map((value) => value.id === panel.id ? panel : value) }
  if (definition.panels.length >= 24) throw new Error("workspace.panel_limit")
  return { ...definition, panels: [...definition.panels, panel] }
}

export function blankWorkspaceDashboard(title: string): MetricDashboard {
  return { id: "new", scope: "personal", revision: 0, has_previous: false, created_at: "", updated_at: "", definition: {
    version: 1, title, description: "", time_range_seconds: 3600, refresh_seconds: 15,
    variables: [{ name: "node", type: "node", label: "Node" }], panels: [],
  } }
}
