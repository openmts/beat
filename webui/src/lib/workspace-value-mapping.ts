import type { DashboardPanel, DashboardValueMapping, PanelColor, ValueMappingType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：每个面板最多 8 条、文本不超过 32 字节、颜色来自固定调色板。 */
export const maximumValueMappings = 8
export const maximumMappingTextBytes = 32
export const valueMappingTypes = ["value", "range", "special"] as const
export const mappingMatchKind = "null" as const

export interface MappedWorkspaceValue { text?: string; color?: PanelColor }

/** 值映射只作用于数值、仪表、表格、条状仪表、饼图与状态时间线；其它类型带上它会被服务端拒绝。 */
export interface MappingFields extends Pick<DashboardPanel, "value_mappings"> {
  /** 导入时面板类型还是原始字符串，因此这里不直接复用 PanelType。 */
  type: string
}

/** mappedWorkspaceValue 返回第一条命中的映射，未命中返回 null，调用方继续按数值与阈值渲染。 */
export function mappedWorkspaceValue(panel: MappingFields, value: number | null): MappedWorkspaceValue | null {
  for (const mapping of panel.value_mappings ?? []) {
    if (!mappingMatches(mapping, value)) continue
    return { ...(mapping.text ? { text: mapping.text } : {}), ...(mapping.color ? { color: mapping.color } : {}) }
  }
  return null
}

function mappingMatches(mapping: DashboardValueMapping, value: number | null): boolean {
  if (mapping.type === "special") return value === null
  if (value === null) return false
  if (mapping.type === "value") return Number.isFinite(mapping.value) && value === mapping.value
  if (mapping.from !== undefined && !(value >= mapping.from)) return false
  return mapping.to === undefined || value < mapping.to
}

/** valueMappingError 返回目录键，null 表示可以保存；规则与服务端 ValidateDashboard 一致。 */
export function valueMappingError(panel: MappingFields): string | null {
  const mappings = panel.value_mappings ?? []
  if (mappings.length === 0) return null
  if (["timeseries", "barchart", "heatmap", "histogram"].includes(panel.type)) return "workspace.value_mappings_timeseries_only"
  if (mappings.length > maximumValueMappings) return "workspace.value_mappings_too_many"
  for (const mapping of mappings) {
    if (!valueMappingTypes.includes(mapping.type)) return "workspace.value_mapping_type_invalid"
    if (!mapping.text && !mapping.color) return "workspace.value_mapping_empty"
    if (mapping.text && new TextEncoder().encode(mapping.text).length > maximumMappingTextBytes) return "workspace.value_mapping_text_too_long"
    if (mapping.text && hasControlCharacters(mapping.text)) return "workspace.value_mapping_text_invalid"
    if (mapping.type === "value" && !Number.isFinite(mapping.value)) return "workspace.value_mapping_value_invalid"
    if (mapping.type === "range") {
      if (mapping.from === undefined && mapping.to === undefined) return "workspace.value_mapping_range_invalid"
      if ([mapping.from, mapping.to].some((bound) => bound !== undefined && !Number.isFinite(bound))) return "workspace.value_mapping_range_invalid"
      if (mapping.from !== undefined && mapping.to !== undefined && mapping.from >= mapping.to) return "workspace.value_mapping_range_invalid"
    }
    if (mapping.type === "special" && mapping.match !== mappingMatchKind) return "workspace.value_mapping_match_invalid"
  }
  return null
}

export function emptyValueMapping(type: ValueMappingType = "value"): DashboardValueMapping {
  if (type === "range") return { type, from: 0, to: 10, color: "amber" }
  if (type === "special") return { type, match: mappingMatchKind, text: "No data", color: "slate" }
  return { type, value: 0, text: "0", color: "rose" }
}

function hasControlCharacters(value: string): boolean {
  return [...value].some((character) => { const code = character.codePointAt(0) ?? 0; return code < 32 || code === 127 })
}

/** 切换映射类型时清理只对旧类型有效的字段，并补上新类型的可编辑默认值。 */
export function switchValueMappingType(mapping: DashboardValueMapping, type: ValueMappingType): DashboardValueMapping {
  return { ...emptyValueMapping(type), ...(mapping.text ? { text: mapping.text } : {}), ...(mapping.color ? { color: mapping.color } : {}) }
}
