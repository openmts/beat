// 覆盖规则的校验与「切换面板类型时清理属性」：规则与服务端 internal/model 的 validateDashboardOverride*
// 一一对应，编辑器与 JSON 导入导出共用这一份判定，避免出现保存后被服务端拒绝的配置。
import { panelColors } from "./workspace-chart"
import { axisSideSupported } from "./workspace-axis-sides"
import { maximumAxisWidth, minimumAxisWidth } from "./workspace-axis-display"
import { validAxisLabel } from "./workspace-axis-label"
import { maximumFillOpacity, maximumLineWidth, minimumLineWidth } from "./workspace-line-style"
import {
  hasControlCharacters, invalidOverrideInteger, maximumOverrideFillBelowBytes, maximumOverridePatternBytes,
  maximumOverrideThresholds, maximumOverrideUnitBytes, maximumPanelOverrides, overrideLimitPanelTypes,
  noValuePanelTypes, overrideMatcherTypeList, overrideSeriesTypeList, overrideStandardPanelTypes,
  overrideThresholdPanelTypes, validNoValue, validOverrideDisplayName, type OverrideFields,
} from "./workspace-override-contract"
import type { DashboardOverride, DashboardPanel, OverrideSeriesType, PanelColor, PanelType } from "@/types/metric-workspace"

/** overrideError 返回目录键，null 表示可以保存；规则与服务端 ValidateDashboard 一致。 */
export function overrideError(panel: OverrideFields): string | null {
  const overrides = panel.overrides ?? []
  if (overrides.length > maximumPanelOverrides) return "workspace.overrides_too_many"
  for (const override of overrides) {
    const failure = oneOverrideError(panel, override)
    if (failure) return failure
  }
  return null
}

/** oneOverrideError 校验单条覆盖：匹配器、属性范围与面板类型的适用性。 */
function oneOverrideError(panel: OverrideFields, override: DashboardOverride): string | null {
  if (!overrideMatcherTypeList.includes(override.matcher?.type)) return "workspace.override_matcher_invalid"
  const value = override.matcher?.value ?? ""
  if (!value.trim() || new TextEncoder().encode(value).length > maximumOverridePatternBytes || hasControlCharacters(value)) {
    return "workspace.override_matcher_invalid"
  }
  if (override.matcher.type === "by_regex" && !isValidExpression(value)) return "workspace.override_matcher_invalid"
  if (override.matcher.type === "by_type" && !overrideSeriesTypeList.includes(value as OverrideSeriesType)) {
    return "workspace.override_series_type_invalid"
  }
  if (override.matcher.type === "by_value" && !Number.isFinite(Number(value))) return "workspace.override_value_invalid"
  if (override.color !== undefined && !Object.prototype.hasOwnProperty.call(panelColors, override.color)) return "workspace.override_color_invalid"
  const unit = override.unit ?? ""
  if (override.unit !== undefined && (!unit.trim() || new TextEncoder().encode(unit).length > maximumOverrideUnitBytes || hasControlCharacters(unit))) {
    return "workspace.override_unit_invalid"
  }
  if (override.line_width !== undefined && invalidOverrideInteger(override.line_width, minimumLineWidth, maximumLineWidth)) {
    return "workspace.override_line_width_invalid"
  }
  if (override.fill_opacity !== undefined && invalidOverrideInteger(override.fill_opacity, 0, maximumFillOpacity)) {
    return "workspace.override_fill_opacity_invalid"
  }
  return optionalOverrideError(panel, override)
}

/** optionalOverrideError 校验与面板类型相关的属性：轴、阈值、填充目标与 Standard options。 */
function optionalOverrideError(panel: OverrideFields, override: DashboardOverride): string | null {
  if (override.axis_placement !== undefined && override.axis_placement !== "left" && override.axis_placement !== "right") {
    return "workspace.override_axis_placement_invalid"
  }
  if (overrideLineStyleUnsupported(panel.type, override)) return "workspace.override_line_style_unsupported"
  const axisFailure = overrideAxisFailure(panel, override)
  if (axisFailure) return axisFailure
  if (overrideThresholdUnsupported(panel.type, override)) return "workspace.override_thresholds_unsupported"
  if (override.thresholds !== undefined && !validOverrideThresholds(override.thresholds)) return "workspace.override_thresholds_invalid"
  const standardFailure = overrideStandardFailure(panel, override)
  if (standardFailure) return standardFailure
  const fillBelowFailure = overrideFillBelowFailure(panel.type, panel.stack, override)
  if (fillBelowFailure) return fillBelowFailure
  if (!overrideCarriesProperty(override)) return "workspace.override_empty"
  return null
}

/** noValueError 校验面板级占位文本（Grafana 的 Standard options → No value）：返回目录键，
 *  null 表示可以保存；规则与服务端 validateDashboardNoValue 一致。 */
export function noValueError(panel: { type?: string; no_value?: string }): string | null {
  const value = panel.no_value
  if (value === undefined || value === "") return null
  if (panel.type !== undefined && !noValuePanelTypes.includes(panel.type)) return "workspace.no_value_unsupported"
  if (!validNoValue(value)) return "workspace.no_value_invalid"
  return null
}

/** noValuePatch 切换面板类型时丢掉新类型不支持的占位文本；与服务端的面板类型门禁一致。 */
export function noValuePatch(type: PanelType): Pick<DashboardPanel, "no_value"> {
  return noValuePanelTypes.includes(type) ? {} : { no_value: undefined }
}

/** overrideCarriesProperty 判定一条覆盖是否至少带了一个属性，避免保存出服务端会拒绝的空规则。 */
export function overrideCarriesProperty(override: DashboardOverride): boolean {
  return override.color !== undefined || override.unit !== undefined || override.hide_legend !== undefined ||
    override.line_width !== undefined || override.fill_opacity !== undefined || override.axis_placement !== undefined ||
    (override.thresholds?.length ?? 0) > 0 || (override.fill_below_to ?? "") !== "" ||
    (override.axis_label ?? "") !== "" || override.axis_soft_min !== undefined || override.axis_soft_max !== undefined ||
    override.axis_width !== undefined || (override.display_name ?? "") !== "" || override.decimals !== undefined ||
    override.min !== undefined || override.max !== undefined
}

/**
 * overrideStandardFailure 校验 Grafana 的 Standard options 覆盖：Display name 与 Decimals 跟着面板
 * 是否给序列渲染名字与数值，Min / Max 只在有单值量程的面板上生效；
 * 返回目录键，null 表示可以保存，与服务端 validateOverrideStandard 同规则。
 */
function overrideStandardFailure(panel: OverrideFields, override: DashboardOverride): string | null {
  const styled = (override.display_name ?? "") !== "" || override.decimals !== undefined ||
    override.min !== undefined || override.max !== undefined
  if (!styled) return null
  if (panel.type !== undefined && !overrideStandardPanelTypes.includes(panel.type)) return "workspace.override_standard_unsupported"
  if ((override.display_name ?? "") !== "" && !validOverrideDisplayName(override.display_name as string)) {
    return "workspace.override_display_name_invalid"
  }
  if (override.decimals !== undefined && invalidOverrideInteger(override.decimals, 0, 6)) return "workspace.override_decimals_invalid"
  if (override.min === undefined && override.max === undefined) return null
  if (panel.type !== undefined && !overrideLimitPanelTypes.includes(panel.type)) return "workspace.override_limits_unsupported"
  if (override.min !== undefined && !Number.isFinite(override.min)) return "workspace.override_limits_invalid"
  if (override.max !== undefined && !Number.isFinite(override.max)) return "workspace.override_limits_invalid"
  if (override.min !== undefined && override.max !== undefined && override.min >= override.max) return "workspace.override_limits_invalid"
  return null
}

/** overrideFillBelowFailure 只在调用方给出面板信息时生效：折线以外没有填充带可画，
 *  堆叠面板按层叠面积绘制，两者都会让「填充到另一条序列」变成渲染器忽略的配置。 */
function overrideFillBelowFailure(type: string | undefined, stack: string | undefined, override: DashboardOverride): string | null {
  if ((override.fill_below_to ?? "") === "") return null
  if (type !== undefined && type !== "timeseries") return "workspace.override_fill_below_unsupported"
  if (stack !== undefined && stack !== "" && stack !== "none") return "workspace.override_fill_below_stacked"
  const target = override.fill_below_to as string
  if (!target.trim() || new TextEncoder().encode(target).length > maximumOverrideFillBelowBytes || hasControlCharacters(target)) {
    return "workspace.override_fill_below_invalid"
  }
  return null
}

/** overrideThresholdUnsupported 只在调用方给出面板类型时生效：逐序列阈值只属于按单值取色的面板。 */
function overrideThresholdUnsupported(type: string | undefined, override: DashboardOverride): boolean {
  return type !== undefined && (override.thresholds?.length ?? 0) > 0 && !overrideThresholdPanelTypes.includes(type)
}

/** validOverrideThresholds 与服务端 validateDashboardThresholds 同规则：≤4 条、颜色合法且严格递增。 */
function validOverrideThresholds(thresholds: { value: number; color: PanelColor }[]): boolean {
  if (thresholds.length > maximumOverrideThresholds) return false
  return thresholds.every((threshold, index) =>
    Number.isFinite(threshold.value) && Object.prototype.hasOwnProperty.call(panelColors, threshold.color) &&
    (index === 0 || threshold.value > thresholds[index - 1]!.value))
}

/** overrideLineStyleUnsupported 只在调用方给出面板类型时生效：线宽只属于折线，填充属于折线与柱状图。 */
function overrideLineStyleUnsupported(type: string | undefined, override: DashboardOverride): boolean {
  if (type === undefined) return false
  if (override.line_width !== undefined && type !== "timeseries") return true
  return override.fill_opacity !== undefined && type !== "timeseries" && type !== "barchart"
}

/**
 * overrideAxisFailure 校验整组轴选项：值轴位置、轴标题与柔性上下限都只属于折线与柱状面板；
 * 柔性上下限要求有限、下限小于上限，并且在面板级固定上下限存在时永远不会生效，因此直接拒绝。
 * 返回目录键，null 表示可以保存，与服务端 validateOverrideAxisOptions 同规则。
 */
function overrideAxisFailure(panel: OverrideFields, override: DashboardOverride): string | null {
  const styled = override.axis_placement !== undefined || (override.axis_label ?? "") !== "" ||
    override.axis_soft_min !== undefined || override.axis_soft_max !== undefined || override.axis_width !== undefined
  if (!styled) return null
  if (panel.type !== undefined && !axisSideSupported(panel.type)) return "workspace.override_axis_placement_unsupported"
  if ((override.axis_label ?? "") !== "" && !validAxisLabel(override.axis_label as string)) return "workspace.axis_label_invalid"
  if (override.axis_soft_min !== undefined && !Number.isFinite(override.axis_soft_min)) return "workspace.soft_axis_invalid"
  if (override.axis_soft_max !== undefined && !Number.isFinite(override.axis_soft_max)) return "workspace.soft_axis_invalid"
  if (override.axis_soft_min !== undefined && override.axis_soft_max !== undefined && override.axis_soft_min >= override.axis_soft_max) return "workspace.soft_axis_order"
  if ((override.axis_soft_min !== undefined || override.axis_soft_max !== undefined) && (panel.min !== undefined || panel.max !== undefined)) return "workspace.soft_axis_conflict"
  if (override.axis_width !== undefined && invalidOverrideInteger(override.axis_width, minimumAxisWidth, maximumAxisWidth)) return "workspace.override_axis_width_invalid"
  return null
}

function isValidExpression(pattern: string): boolean {
  try {
    new RegExp(pattern)
    return true
  } catch (_) {
    void _
    return false
  }
}

/** overrideTypePatch 切换面板类型时丢掉新类型渲染不了的覆盖属性；只带这些属性的覆盖整条移除，
 *  与面板级线宽/填充在切换类型时被清理的口径一致，避免保存出渲染器忽略的配置。 */
export function overrideTypePatch(type: PanelType, overrides: DashboardOverride[] | undefined): Pick<DashboardPanel, "overrides"> {
  if (!overrides?.length) return {}
  const kept = overrides.map((override) => pruneOverride(type, override)).filter(overrideCarriesProperty)
  return kept.length ? { overrides: kept } : { overrides: [] }
}

/** pruneOverride 去掉新面板类型渲染不了的单条覆盖属性。 */
function pruneOverride(type: PanelType, override: DashboardOverride): DashboardOverride {
  const next: DashboardOverride = { ...override }
  if (type !== "timeseries") delete next.line_width
  if (type !== "timeseries" && type !== "barchart") delete next.fill_opacity
  if (!axisSideSupported(type)) {
    delete next.axis_placement
    delete next.axis_label
    delete next.axis_soft_min
    delete next.axis_soft_max
    delete next.axis_width
  }
  if (!overrideThresholdPanelTypes.includes(type)) delete next.thresholds
  if (type !== "timeseries") delete next.fill_below_to
  if (!overrideStandardPanelTypes.includes(type)) {
    delete next.display_name
    delete next.decimals
    delete next.min
    delete next.max
  }
  if (!overrideLimitPanelTypes.includes(type)) {
    delete next.min
    delete next.max
  }
  return next
}

/** fillBelowStackPatch 打开堆叠时丢掉新类型渲染不了的「填充到另一条序列」；只带它的覆盖整条移除。 */
export function fillBelowStackPatch(stack: string | undefined, overrides: DashboardOverride[] | undefined): Pick<DashboardPanel, "overrides"> {
  if ((stack !== "normal" && stack !== "percent") || !overrides?.some((override) => override.fill_below_to)) return {}
  const kept = overrides.map((override) => {
    const next: DashboardOverride = { ...override }
    delete next.fill_below_to
    return next
  }).filter(overrideCarriesProperty)
  return { overrides: kept }
}
