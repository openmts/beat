import { variableRegexError } from "@/lib/workspace-variable-options"
import type { DashboardDefinition, DashboardVariable } from "@/types/metric-workspace"

export interface WorkspaceVariableDraft { id: string; original?: string; value: DashboardVariable }

/** validWorkspaceVariable 与服务端同规则：名称受限、显示名非空、正则可编译且不超过 64 字节。 */
function validWorkspaceVariable(variable: DashboardVariable): boolean {
  if (!/^[a-z][a-z0-9_]{0,31}$/.test(variable.name) || ["constructor", "prototype", "__proto__"].includes(variable.name)) return false
  if (!variable.label.trim() || new TextEncoder().encode(variable.label).length > 80) return false
  if (variable.include_all === true && variable.multi !== true) return false
  return variableRegexError(variable.regex ?? "") === null
}

export function applyWorkspaceVariables(definition: DashboardDefinition, drafts: WorkspaceVariableDraft[]): DashboardDefinition {
  const names = drafts.map((draft) => draft.value.name)
  if (drafts.length > 4 || new Set(names).size !== names.length || drafts.some((draft) => !validWorkspaceVariable(draft.value))) throw new Error("workspace.invalid_variable")
  const replacements = new Map(drafts.filter((draft) => draft.original).map((draft) => [`$${draft.original}`, `$${draft.value.name}`]))
  const panels = definition.panels.map((panel) => ({ ...panel, series: panel.series.map((series) => ({ ...series, node_id: replacements.get(series.node_id) ?? series.node_id })) }))
  if (panels.some((panel) => panel.series.some((series) => series.node_id.startsWith("$") && !names.includes(series.node_id.slice(1))))) throw new Error("workspace.variable_in_use")
  return { ...definition, variables: drafts.map((draft) => draft.value), panels }
}
