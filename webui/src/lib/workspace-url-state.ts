import type { DashboardVariable, WorkspaceSelections } from "@/types/metric-workspace"

/** 变量选择进 URL 时统一加前缀，避免和 dashboard、node_id 这类控制参数撞名。 */
export const workspaceVariablePrefix = "var-"
// URL 是外部输入：单个变量的取值数量与服务端变量数量同量级留出余量，只拦明显异常的超长串。
const maximumParameterValues = 200

/** 从地址栏读取变量选择；单选变量只认第一个取值，不能靠手改 URL 绕过多选开关。 */
export function workspaceVariableSelections(variables: DashboardVariable[], search: URLSearchParams): WorkspaceSelections {
  const selections: WorkspaceSelections = {}
  for (const variable of variables) {
    const raw = search.get(`${workspaceVariablePrefix}${variable.name}`)
    if (!raw) continue
    const values = raw.split(",").map((value) => value.trim()).filter(Boolean).slice(0, maximumParameterValues)
    if (values.length === 0) continue
    selections[variable.name] = variable.multi === true ? values : values.slice(0, 1)
  }
  return selections
}

/** 变量的 URL 参数：空选择不写，单选变量只写第一个取值。 */
export function workspaceVariableParameters(variables: DashboardVariable[], selections: WorkspaceSelections): [string, string][] {
  const parameters: [string, string][] = []
  for (const variable of variables) {
    const values = (selections[variable.name] ?? []).filter(Boolean)
    if (values.length === 0) continue
    const joined = (variable.multi === true ? values : values.slice(0, 1)).slice(0, maximumParameterValues).join(",")
    if (joined) parameters.push([`${workspaceVariablePrefix}${variable.name}`, joined])
  }
  return parameters
}

/** 覆盖式写入：先清掉旧变量键，再写当前选择；其余控制参数（dashboard、node_id）原样保留。 */
export function workspaceVariableSearch(variables: DashboardVariable[], selections: WorkspaceSelections, previous: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(previous)
  for (const key of [...next.keys()]) if (key.startsWith(workspaceVariablePrefix)) next.delete(key)
  for (const [key, value] of workspaceVariableParameters(variables, selections)) next.set(key, value)
  return next
}
