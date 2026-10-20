import type { Group, Node } from "@/types"
import type { DashboardVariable } from "@/types/metric-workspace"

export interface WorkspaceVariableOption { value: string; label: string }

/** All 使用保留哨兵，查询展开时才绑定到当前已授权且过滤后的全部选项。 */
export const workspaceAllSelection = "$__all"

/** 与 Go `model.MaxDashboardVariableRegexBytes` 一致：正则只过滤已授权的选项，不能无界。 */
export const maximumVariableRegexBytes = 64

/** workspaceVariableOptions 收集变量可选项，并按变量的正则过滤；空或非法正则不做过滤。 */
export function workspaceVariableOptions(variable: DashboardVariable, nodes: Node[], groups: Group[]): WorkspaceVariableOption[] {
  const options = variable.type === "node"
    ? nodes.map((node) => ({ value: node.id, label: node.alias || node.name }))
    : groups.map((group) => ({ value: group.id, label: group.name }))
  return filterWorkspaceOptions(options, variable.regex)
}

/** 变量当前选择解析为选项 ID 集合；All 只展开为此时已授权且通过过滤的选项。 */
export function workspaceVariableSelection(variable: DashboardVariable, selected: string[], nodes: Node[], groups: Group[]): Set<string> {
  if (selected.length === 0) return new Set()
  if (variable.include_all === true && selected.includes(workspaceAllSelection)) {
    return new Set(workspaceVariableOptions(variable, nodes, groups).map((option) => option.value))
  }
  return new Set(selected)
}

export function filterWorkspaceOptions(options: WorkspaceVariableOption[], regex: string | undefined): WorkspaceVariableOption[] {
  const pattern = workspaceOptionPattern(regex)
  return pattern ? options.filter((option) => pattern.test(option.label)) : options
}

/** variableRegexError 返回编辑期可翻译的错误键；合法或留空时返回 null。 */
export function variableRegexError(regex: string): string | null {
  if (!regex) return null
  if (new TextEncoder().encode(regex).length > maximumVariableRegexBytes) return "workspace.variable_regex_too_long"
  return workspaceOptionPattern(regex) ? null : "workspace.variable_regex_invalid"
}

/** 只接受长度受限且可编译的正则；其余情况按“不过滤”处理，渲染永不因看板数据抛错。 */
function workspaceOptionPattern(regex: string | undefined): RegExp | null {
  if (!regex || new TextEncoder().encode(regex).length > maximumVariableRegexBytes) return null
  try { return new RegExp(regex) } catch { return null }
}
