import { api } from "@/lib/api-client"
import { withWorkspaceReadSlot } from "@/lib/workspace-read-gate"
import type { DashboardDefinition, DashboardPanel, PanelColor } from "@/types/metric-workspace"

export interface DashboardAnnotation {
  id: string
  dashboard_id: string
  panel_id?: string
  title: string
  text?: string
  tags: string[]
  color: PanelColor
  visibility: "private" | "public"
  from: string
  to: string
  created_at: string
  updated_at: string
  created_by?: string
}

export interface DashboardSnapshot {
  id: string
  token?: string
  name: string
  dashboard_id: string
  revision: number
  definition: DashboardDefinition
  expires_at?: string | null
  revoked_at?: string | null
  created_at: string
  created_by?: string
}

export interface DashboardLibraryPanel {
  id: string
  name: string
  description?: string
  panel: DashboardPanel
  created_at: string
  updated_at: string
}

export interface PanelAlertState {
  phase: string
  status: string
  reason?: string
  firing: number
  pending: number
  no_data: number
  error: number
  normal: number
  updated_at?: string | null
}

export interface DashboardPanelAlert {
  dashboard_id: string
  panel_id: string
  rule_id: string
  rule_name: string
  metric: string
  severity: string
  enabled: boolean
  created_at: string
  state?: PanelAlertState | null
}

export interface AnnotationDraft {
  dashboard_id: string
  panel_id?: string
  title: string
  text?: string
  tags: string[]
  color: PanelColor
  visibility: "private" | "public"
  from: string
  to: string
}

export async function listAnnotations(dashboardId: string, from: string, to: string, signal: AbortSignal): Promise<DashboardAnnotation[]> {
  return (await withWorkspaceReadSlot(() => api.get<DashboardAnnotation[]>("/admin/metrics/annotations", {
    params: { dashboard_id: dashboardId, from, to }, signal,
  }))).data
}

export async function createAnnotation(draft: AnnotationDraft): Promise<DashboardAnnotation> {
  return (await api.post<DashboardAnnotation>("/admin/metrics/annotations", draft)).data
}

export async function updateAnnotation(id: string, draft: AnnotationDraft): Promise<DashboardAnnotation> {
  return (await api.put<DashboardAnnotation>(`/admin/metrics/annotations/${encodeURIComponent(id)}`, draft)).data
}

export async function deleteAnnotation(id: string): Promise<void> {
  await api.delete(`/admin/metrics/annotations/${encodeURIComponent(id)}`)
}

export async function listSnapshots(): Promise<DashboardSnapshot[]> {
  return (await api.get<DashboardSnapshot[]>("/admin/metrics/snapshots")).data
}

export async function createSnapshot(input: { dashboardId: string; name: string; expiresInSeconds: number }): Promise<DashboardSnapshot> {
  return (await api.post<DashboardSnapshot>("/admin/metrics/snapshots", {
    dashboard_id: input.dashboardId, name: input.name, expires_in_seconds: input.expiresInSeconds,
  })).data
}

export async function revokeSnapshot(id: string): Promise<void> {
  await api.delete(`/admin/metrics/snapshots/${encodeURIComponent(id)}`)
}

export async function getSnapshot(token: string, signal: AbortSignal): Promise<DashboardSnapshot> {
  return (await api.get<DashboardSnapshot>(`/metrics/snapshots/${encodeURIComponent(token)}`, { signal })).data
}

export async function listLibraryPanels(): Promise<DashboardLibraryPanel[]> {
  return (await api.get<DashboardLibraryPanel[]>("/admin/metrics/panels")).data
}

export async function createLibraryPanel(input: { name: string; description?: string; panel: DashboardPanel }): Promise<DashboardLibraryPanel> {
  return (await api.post<DashboardLibraryPanel>("/admin/metrics/panels", input)).data
}

export async function deleteLibraryPanel(id: string): Promise<void> {
  await api.delete(`/admin/metrics/panels/${encodeURIComponent(id)}`)
}

export async function listPanelAlerts(dashboardId: string, signal: AbortSignal): Promise<DashboardPanelAlert[]> {
  return (await withWorkspaceReadSlot(() => api.get<DashboardPanelAlert[]>(`/admin/metrics/dashboards/${encodeURIComponent(dashboardId)}/panel-alerts`, { signal }))).data
}

export async function replacePanelAlerts(dashboardId: string, panelId: string, ruleIds: string[]): Promise<DashboardPanelAlert[]> {
  return (await api.put<DashboardPanelAlert[]>(
    `/admin/metrics/dashboards/${encodeURIComponent(dashboardId)}/panel-alerts/${encodeURIComponent(panelId)}`,
    { rule_ids: ruleIds },
  )).data
}
