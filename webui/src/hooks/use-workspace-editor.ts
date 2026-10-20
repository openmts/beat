import { useState } from "react"
import { createMetricDashboard, restoreDashboardVersion, rollbackMetricDashboard, saveMetricDashboard } from "@/lib/metric-workspace-api"
import { blankWorkspaceDashboard, workspaceDirty, type WorkspaceEditorState } from "@/lib/workspace-editor"
import { performEditorMutation, reloadEditorDashboard, removeEditorDashboard } from "@/lib/workspace-editor-actions"
import type { DashboardDefinition, MetricDashboard } from "@/types/metric-workspace"

export function useWorkspaceEditor(dashboards: MetricDashboard[], initialId?: string) {
  const initial = dashboards.find((value) => value.id === initialId) ?? dashboards.find((value) => value.id === "admin-default") ?? dashboards[0]
  const [state, setState] = useState<WorkspaceEditorState>(() => ({ dashboards, record: structuredClone(initial), saved: structuredClone(initial), busy: false, conflict: false }))
  const dirty = workspaceDirty(state)
  function select(id: string) {
    const selected = state.dashboards.find((value) => value.id === id)
    if (selected) setState((current) => ({ ...current, record: structuredClone(selected), saved: structuredClone(selected), error: undefined, conflict: false }))
  }
  const perform = (task: () => Promise<MetricDashboard>) => performEditorMutation(state, setState, task)
  const adopt = (definition: DashboardDefinition) => setState((current) => ({ ...current, record: { ...blankWorkspaceDashboard(definition.title), definition }, error: undefined, conflict: false }))
  return { ...state, dirty, select, adopt, remove: () => removeEditorDashboard(state, setState), reload: () => reloadEditorDashboard(state, setState),
    update: (change: (definition: DashboardDefinition) => DashboardDefinition) => setState((current) => ({ ...current, record: { ...current.record, definition: change(current.record.definition) } })),
    discard: () => setState((current) => ({ ...current, record: structuredClone(current.saved), error: undefined, conflict: false })),
    create: (title: string) => setState((current) => ({ ...current, record: blankWorkspaceDashboard(title), error: undefined, conflict: false })),
    save: (copy = false) => perform(() => copy || state.record.id === "new" ? createMetricDashboard(state.record.definition) : saveMetricDashboard(state.record)),
    duplicate: (title: string) => perform(() => createMetricDashboard({ ...state.record.definition, title })),
    rollback: () => perform(() => rollbackMetricDashboard(state.record)),
    restoreVersion: (revision: number, summary = "") => perform(() => restoreDashboardVersion(state.record, revision, summary)),
    publish: (target: MetricDashboard) => perform(() => saveMetricDashboard({ ...target, definition: state.record.definition })),
  }
}

export type WorkspaceEditor = ReturnType<typeof useWorkspaceEditor>
