import type { PublicWorkspaceSources } from "@/lib/public-workspace-api"
import type { DashboardDefinition, WorkspaceRange, WorkspaceSelections } from "@/types/metric-workspace"

export const emptyPublicDefinition: DashboardDefinition = { version: 1, title: "", description: "", time_range_seconds: 3600, refresh_seconds: 0, variables: [], panels: [] }
export interface PublicWorkspaceControls { key: string; range: WorkspaceRange; refreshSeconds: number; values: WorkspaceSelections }

export function initialPublicControls(nodeId: string | undefined, sources: PublicWorkspaceSources | null): PublicWorkspaceControls {
  const node = sources?.nodes.find((value) => value.id === nodeId)
  const definition = sources?.dashboard.definition ?? emptyPublicDefinition
  return { key: `${nodeId ?? ""}:${sources?.dashboard.id ?? ""}:${sources?.dashboard.revision ?? ""}`,
    range: { mode: "relative", seconds: definition.time_range_seconds }, refreshSeconds: definition.refresh_seconds,
    values: Object.fromEntries(definition.variables.map((variable) => {
      const selected = variable.type === "node" ? node?.id : node?.group_id
      return [variable.name, selected ? [selected] : []]
    })) }
}
