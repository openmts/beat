import { record, text, finite, importBoundedInteger, importNumber, panelColorValue, importThresholds } from "@/lib/workspace-import-core"
import { maximumFillOpacity, maximumLineWidth, minimumLineWidth } from "@/lib/workspace-line-style"
import { maximumAxisWidth, minimumAxisWidth } from "@/lib/workspace-axis-display"
import { maximumFrameColumnBytes } from "@/lib/workspace-frame-ops"
import { panelCalculations } from "@/lib/workspace-calculation"
import { maximumMappingTextBytes, maximumValueMappings, mappingMatchKind, valueMappingError, valueMappingTypes } from "@/lib/workspace-value-mapping"
import { maximumPanelOverrides, overrideError, overrideMatcherTypeList } from "@/lib/workspace-overrides"
import { maximumPanelTransformations, maximumTransformationCalculations, maximumTransformationPatternBytes, maximumTransformationReplacementBytes, transformationComparisons, transformationError, transformationGroupCalculations, transformationGroupFields, transformationKinds, transformationOperators, transformationOrders, transformationReduceModes, transformationStatistics } from "@/lib/workspace-transformations"
import type { DashboardOverride, DashboardPanel, DashboardTransformation, DashboardValueMapping, PanelCalculation, PanelType, TransformationGroupCalculation } from "@/types/metric-workspace"

/** 结果变换随看板导入导出：种类、参数或字段混用不合法都让整份文件按无效处理。 */
export function importTransformations(source: Record<string, unknown>): Pick<DashboardPanel, "transformations"> {
  if (source.transformations === undefined) return {}
  if (!Array.isArray(source.transformations) || source.transformations.length > maximumPanelTransformations) throw new Error("workspace.import_invalid")
  const transformations = source.transformations.map(importTransformation)
  const panel: Pick<DashboardPanel, "transformations"> = transformations.length ? { transformations } : {}
  if (transformationError(panel)) throw new Error("workspace.import_invalid")
  return panel
}

/** 每条变换只接受自己那一组字段；服务端同样拒绝字段混用的负载。 */
export function transformationKeys(kind: DashboardTransformation["kind"]): string[] {
  if (kind === "rename_by_regex") return ["kind", "pattern", "replacement"]
  if (kind === "filter_by_value") return ["kind", "comparison", "value", "column"]
  if (kind === "binary_operation") return ["kind", "left", "operator", "right", "alias"]
  if (kind === "reduce") return ["kind", "calculation", "calculations", "mode"]
  if (kind === "group_by") return ["kind", "group_by", "calculation", "calculations", "alias"]
  return kind === "sort_by" ? ["kind", "statistic", "field", "order"] : ["kind", "count"]
}

export function importTransformation(entry: unknown): DashboardTransformation {
  const source = record(entry), kind = transformationKinds.find((candidate) => candidate === source?.kind)
  if (!source || !kind) throw new Error("workspace.import_invalid")
  if (Object.keys(source).some((key) => !transformationKeys(kind).includes(key))) throw new Error("workspace.import_invalid")
  if (kind === "rename_by_regex") {
    if (typeof source.pattern !== "string" || !source.pattern || new TextEncoder().encode(source.pattern).length > maximumTransformationPatternBytes) throw new Error("workspace.import_invalid")
    const replacement = source.replacement === undefined ? "" : text(source.replacement, maximumTransformationReplacementBytes)
    if (replacement === null) throw new Error("workspace.import_invalid")
    return { kind, pattern: source.pattern, replacement }
  }
  if (kind === "filter_by_value") {
    const comparison = transformationComparisons.find((candidate) => candidate === source.comparison)
    if (!comparison) throw new Error("workspace.import_invalid")
    // column 是可选的结果帧列选择：给了它这一步改为按列过滤行，没给保持历史口径。
    const column = source.column === undefined ? undefined : text(source.column, maximumFrameColumnBytes)
    if (column === null) throw new Error("workspace.import_invalid")
    return { kind, comparison, value: importNumber(source.value), ...(column ? { column } : {}) }
  }
  if (kind === "sort_by") {
    // statistic 与 field 至少给一个：前者按图例统计量的同名计算列排序，后者直接指定结果帧的列。
    const statistic = transformationStatistics.find((candidate) => candidate === source.statistic)
    if (source.statistic !== undefined && !statistic) throw new Error("workspace.import_invalid")
    const field = source.field === undefined ? undefined : text(source.field, maximumFrameColumnBytes)
    if (field === null || (!statistic && !field)) throw new Error("workspace.import_invalid")
    const order = transformationOrders.find((candidate) => candidate === source.order)
    if (!order) throw new Error("workspace.import_invalid")
    return { kind, statistic, order, ...(field ? { field } : {}) }
  }
  if (kind === "reduce") {
    // 单个 calculation 是历史写法，calculations 是新的多选写法；两者只接受一种。
    if (source.calculation !== undefined && source.calculations !== undefined) throw new Error("workspace.import_invalid")
    const mode = source.mode === undefined ? undefined : transformationReduceModes.find((candidate) => candidate === source.mode)
    if (source.mode !== undefined && !mode) throw new Error("workspace.import_invalid")
    if (source.calculation !== undefined) {
      const calculation = panelCalculations.find((candidate) => candidate === source.calculation)
      if (!calculation) throw new Error("workspace.import_invalid")
      return { kind, calculation, ...(mode ? { mode } : {}) }
    }
    if (!Array.isArray(source.calculations) || source.calculations.length === 0 || source.calculations.length > maximumTransformationCalculations) {
      throw new Error("workspace.import_invalid")
    }
    const calculations = source.calculations.map((candidate) => panelCalculations.find((value) => value === candidate))
    if (calculations.some((candidate) => !candidate) || new Set(calculations).size !== calculations.length) throw new Error("workspace.import_invalid")
    if (calculations.length > 1 && mode !== "series_to_rows") throw new Error("workspace.import_invalid")
    return { kind, calculations: calculations as PanelCalculation[], ...(mode ? { mode } : {}) }
  }
  if (kind === "group_by") {
    // 单个 calculation 是历史写法，calculations 是新的多选写法；两者只接受一种，alias 是可选命名。
    const groupBy = transformationGroupFields.find((candidate) => candidate === source.group_by)
    if (!groupBy || (source.calculation !== undefined && source.calculations !== undefined)) throw new Error("workspace.import_invalid")
    const alias = source.alias === undefined ? "" : text(source.alias, maximumTransformationReplacementBytes)
    if (alias === null) throw new Error("workspace.import_invalid")
    if (source.calculation !== undefined) {
      const calculation = transformationGroupCalculations.find((candidate) => candidate === source.calculation)
      if (!calculation) throw new Error("workspace.import_invalid")
      return { kind, group_by: groupBy, calculation, ...(alias ? { alias } : {}) }
    }
    if (!Array.isArray(source.calculations) || source.calculations.length === 0 || source.calculations.length > maximumTransformationCalculations) {
      throw new Error("workspace.import_invalid")
    }
    const calculations = source.calculations.map((candidate) => transformationGroupCalculations.find((value) => value === candidate))
    if (calculations.some((candidate) => !candidate) || new Set(calculations).size !== calculations.length) throw new Error("workspace.import_invalid")
    return { kind, group_by: groupBy, calculations: calculations as TransformationGroupCalculation[], ...(alias ? { alias } : {}) }
  }
  if (kind === "binary_operation") {
    const operator = transformationOperators.find((candidate) => candidate === source.operator)
    if (typeof source.left !== "string" || typeof source.right !== "string" || !operator) throw new Error("workspace.import_invalid")
    const alias = source.alias === undefined ? "" : text(source.alias, maximumTransformationReplacementBytes)
    if (alias === null) throw new Error("workspace.import_invalid")
    // 选择器与别名的合法性由外层 transformationError 统一把关，与编辑器保持同一套规则。
    return { kind, left: source.left, operator, right: source.right, ...(alias ? { alias } : {}) }
  }
  const count = finite(source.count)
  if (count === null || !Number.isInteger(count) || count < 1 || count > 8) throw new Error("workspace.import_invalid")
  return { kind, count }
}

/** 覆盖里允许出现的键：与编辑器写入的字段一致。 */
export const overrideKeys = ["matcher", "color", "unit", "hide_legend", "line_width", "fill_opacity", "axis_placement", "thresholds", "fill_below_to",
  "axis_label", "axis_soft_min", "axis_soft_max", "axis_width", "display_name", "decimals", "min", "max"]

/** 字段覆盖随看板导入导出：匹配器、属性或空规则不合法都让整份文件按无效处理。 */
export function importOverrides(source: Record<string, unknown>, type: PanelType, stack: string | undefined,
  min?: number, max?: number): Pick<DashboardPanel, "overrides"> {
  if (source.overrides === undefined) return {}
  if (!Array.isArray(source.overrides) || source.overrides.length > maximumPanelOverrides) throw new Error("workspace.import_invalid")
  const overrides = source.overrides.map(importOverride)
  // 行与文本面板不渲染序列，服务端会拒绝它们的覆盖，所以这里同样按无效文件处理；
  // 堆叠面板也会拒绝「填充到另一条序列」，导入侧用同一套规则把关。
  if (type === "row" || type === "text" || overrideError({ type, stack, overrides, min, max })) throw new Error("workspace.import_invalid")
  // 显式空数组与缺省同义：两者都表示这块面板没有覆盖，导入后不落这个字段。
  return overrides.length ? { overrides } : {}
}

/** 每条覆盖只接受 matcher/color/unit/hide_legend/line_width/fill_opacity/axis_placement/thresholds/fill_below_to；
 *  选择器、属性范围与面板类型的匹配交给 overrideError 统一裁决。 */
export function importOverride(value: unknown): DashboardOverride {
  const source = record(value), matcher = record(source?.matcher)
  if (!source || !matcher) throw new Error("workspace.import_invalid")
  if (Object.keys(source).some((key) => !overrideKeys.includes(key))) throw new Error("workspace.import_invalid")
  if (Object.keys(matcher).some((key) => key !== "type" && key !== "value")) throw new Error("workspace.import_invalid")
  const matcherType = overrideMatcherTypeList.find((candidate) => candidate === matcher.type)
  if (!matcherType || typeof matcher.value !== "string") throw new Error("workspace.import_invalid")
  const override: DashboardOverride = { matcher: { type: matcherType, value: matcher.value } }
  if (source.color !== undefined) override.color = panelColorValue(source.color)
  if (source.unit !== undefined) {
    if (typeof source.unit !== "string" || source.unit === "") throw new Error("workspace.import_invalid")
    override.unit = source.unit
  }
  if (source.hide_legend !== undefined) {
    if (typeof source.hide_legend !== "boolean") throw new Error("workspace.import_invalid")
    override.hide_legend = source.hide_legend
  }
  if (source.line_width !== undefined) override.line_width = importBoundedInteger(source.line_width, minimumLineWidth, maximumLineWidth)
  if (source.fill_opacity !== undefined) override.fill_opacity = importBoundedInteger(source.fill_opacity, 0, maximumFillOpacity)
  if (source.axis_placement !== undefined) override.axis_placement = overrideAxisSide(source.axis_placement)
  if (source.fill_below_to !== undefined) {
    if (typeof source.fill_below_to !== "string" || source.fill_below_to === "") throw new Error("workspace.import_invalid")
    override.fill_below_to = source.fill_below_to
  }
  if (source.axis_label !== undefined) {
    if (typeof source.axis_label !== "string" || source.axis_label === "") throw new Error("workspace.import_invalid")
    override.axis_label = source.axis_label
  }
  if (source.axis_soft_min !== undefined) override.axis_soft_min = importNumber(source.axis_soft_min)
  if (source.axis_soft_max !== undefined) override.axis_soft_max = importNumber(source.axis_soft_max)
  if (source.axis_width !== undefined) override.axis_width = importBoundedInteger(source.axis_width, minimumAxisWidth, maximumAxisWidth)
  if (source.display_name !== undefined) {
    if (typeof source.display_name !== "string" || source.display_name === "") throw new Error("workspace.import_invalid")
    override.display_name = source.display_name
  }
  if (source.decimals !== undefined) override.decimals = importBoundedInteger(source.decimals, 0, 6)
  if (source.min !== undefined) override.min = importNumber(source.min)
  if (source.max !== undefined) override.max = importNumber(source.max)
  // 空数组等同于「没有阈值」，导出侧从来不写：出现时按无效文件处理，避免留下服务端会拒绝的空规则。
  if (source.thresholds !== undefined) {
    if (!Array.isArray(source.thresholds) || source.thresholds.length === 0) throw new Error("workspace.import_invalid")
    override.thresholds = importThresholds(source.thresholds)
  }
  return override
}

/** overrideAxisSide 只接受左右两个取值，其余一律按无效文件处理。 */
export function overrideAxisSide(value: unknown): "left" | "right" {
  if (value === "left" || value === "right") return value
  throw new Error("workspace.import_invalid")
}

/** 值映射随看板导入导出：类型、参数、文本或颜色不合法都让整份文件按无效处理。 */
export function importValueMappings(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "value_mappings"> {
  if (source.value_mappings === undefined) return {}
  if (!Array.isArray(source.value_mappings) || source.value_mappings.length > maximumValueMappings) throw new Error("workspace.import_invalid")
  const mappings = source.value_mappings.map(importValueMapping)
  const panel: Pick<DashboardPanel, "type" | "value_mappings"> = { type, ...(mappings.length ? { value_mappings: mappings } : {}) }
  if (valueMappingError(panel)) throw new Error("workspace.import_invalid")
  return mappings.length ? { value_mappings: mappings } : {}
}

export function importValueMapping(entry: unknown): DashboardValueMapping {
  const source = record(entry), kind = valueMappingTypes.find((candidate) => candidate === source?.type)
  if (!source || !kind) throw new Error("workspace.import_invalid")
  const text = typeof source.text === "string" && source.text.length <= maximumMappingTextBytes ? source.text : null
  if (text === null) throw new Error("workspace.import_invalid")
  const color = source.color === undefined ? undefined : panelColorValue(source.color)
  const mapping: DashboardValueMapping = { type: kind, ...(text ? { text } : {}), ...(color ? { color } : {}) }
  if (kind === "value") mapping.value = importNumber(source.value)
  if (kind === "range") {
    if (source.from !== undefined) mapping.from = importNumber(source.from)
    if (source.to !== undefined) mapping.to = importNumber(source.to)
  }
  if (kind === "special") {
    if (source.match !== mappingMatchKind) throw new Error("workspace.import_invalid")
    mapping.match = mappingMatchKind
  }
  return mapping
}
