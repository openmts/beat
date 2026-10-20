import type { Dispatch, SetStateAction } from "react"
import { APIError } from "@/lib/api-error"
import { deleteMetricDashboard, listMetricDashboards } from "@/lib/metric-workspace-api"
import { applyDashboardSave, type WorkspaceEditorState } from "@/lib/workspace-editor"
import type { MetricDashboard } from "@/types/metric-workspace"

type EditorUpdate = Dispatch<SetStateAction<WorkspaceEditorState>>

export async function performEditorMutation(state: WorkspaceEditorState, update: EditorUpdate, task: () => Promise<MetricDashboard>): Promise<void> {
  update((current) => ({ ...current, busy: true, error: undefined, conflict: false }))
  try { const saved = await task(); update((current) => applyDashboardSave(current, saved, state.record)) }
  catch (error) { failEditorMutation(update, error) }
}

export async function removeEditorDashboard(state: WorkspaceEditorState, update: EditorUpdate): Promise<void> {
  update((current) => ({ ...current, busy: true, error: undefined }))
  try {
    await deleteMetricDashboard(state.record)
    update((current) => {
      const dashboards = current.dashboards.filter((value) => value.id !== state.record.id)
      const next = dashboards.find((value) => value.id === "admin-default") ?? dashboards[0]
      return { record: structuredClone(next), saved: structuredClone(next), dashboards, busy: false, conflict: false }
    })
  } catch (error) { failEditorMutation(update, error) }
}

export async function reloadEditorDashboard(state: WorkspaceEditorState, update: EditorUpdate): Promise<void> {
  update((current) => ({ ...current, busy: true, error: undefined }))
  try {
    const dashboards = await listMetricDashboards()
    const next = dashboards.find((value) => value.id === state.record.id) ?? dashboards.find((value) => value.id === "admin-default") ?? dashboards[0]
    update((current) => ({ ...current, dashboards, record: structuredClone(next), saved: structuredClone(next), busy: false, conflict: false }))
  } catch (error) { failEditorMutation(update, error) }
}

function failEditorMutation(update: EditorUpdate, error: unknown) {
  update((current) => ({ ...current, busy: false, conflict: error instanceof APIError && error.status === 409,
    error: error instanceof Error ? error.message : "workspace.save_failed" }))
}
