import { maximumVariableRegexBytes, variableRegexError } from "@/lib/workspace-variable-options"
import { dashboardTagError, maximumDashboardTags } from "@/lib/workspace-dashboard-tags"
import { maximumNowDelaySeconds, maximumQueryPoints, minimumQueryPoints } from "@/lib/workspace-query"
import type { DashboardPanel, DashboardVariable, LegendSort, LegendValue, MetricAggregation, PanelColor, PanelSize, PanelType, TooltipMode } from "@/types/metric-workspace"

export const panelTypes: PanelType[] = ["timeseries", "barchart", "stat", "gauge", "bargauge", "piechart", "heatmap", "histogram", "geomap", "statetimeline", "table", "text", "row"]
export const panelSizes: PanelSize[] = ["small", "medium", "large"]
export const panelColors: PanelColor[] = ["blue", "green", "amber", "rose", "violet", "cyan", "slate"]
export const aggregations: MetricAggregation[] = ["avg", "min", "max", "sum", "last", "rate"]
export const legendValueKinds: LegendValue[] = ["last", "min", "max", "mean"]
export const legendSortModes: LegendSort[] = ["none", "asc", "desc"]
export const tooltipChoices: TooltipMode[] = ["all", "single", "off"]
export const refreshChoices = [0, 5, 10, 15, 30, 60, 300]
export const limits = { panels: 24, series: 8, variables: 4, thresholds: 4, legend: 4, range: 31 * 24 * 60 * 60, title: 120, description: 2048, label: 120, variableLabel: 80, metric: 128, key: 64 }
export const keyPattern = /^[a-zA-Z0-9_-]{1,64}$/, metricPattern = /^[a-zA-Z0-9_.-]{1,128}$/, variablePattern = /^[a-z][a-z0-9_]{0,31}$/
export const reservedKeys = ["__proto__", "prototype", "constructor"]

export interface DashboardCandidate { source: Record<string, unknown>; variables: unknown[]; panels: unknown[] }

export function definitionCandidate(parsed: unknown): DashboardCandidate | null {
  const root = record(parsed)
  if (!root) return null
  const source = record("definition" in root ? root.definition : root)
  if (!source || source.version !== 1 || !Array.isArray(source.panels) || !Array.isArray(source.variables)) return null
  return { source, variables: source.variables, panels: source.panels }
}

export function record(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : null
}

export function hasControlCharacters(value: string): boolean {
  return [...value].some((character) => { const code = character.codePointAt(0) ?? 0; return code < 32 || code === 127 })
}

export function text(value: unknown, limit: number): string | null {
  if (typeof value !== "string" || value.trim() === "" || value.length > limit || hasControlCharacters(value)) return null
  return value
}

export function finite(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null
}

export function importTitle(value: unknown): string {
  const title = text(value, limits.title)
  if (!title) throw new Error("workspace.import_invalid")
  return title
}

export function importDescription(value: unknown): string {
  return typeof value === "string" && value.length <= limits.description && !hasControlCharacters(value) ? value : ""
}

export function importRange(value: unknown): number {
  const seconds = finite(value)
  return seconds !== null && seconds >= 60 && seconds <= limits.range ? Math.round(seconds) : 3600
}

export function importRefresh(value: unknown): number {
  const seconds = finite(value)
  return seconds !== null && refreshChoices.includes(seconds) ? seconds : 15
}

/** 检索标签随导出导入：数量、字符集或重复问题都让整份文件按无效处理。 */
export function importTags(value: unknown): string[] {
  if (value === undefined) return []
  if (!Array.isArray(value) || value.length > maximumDashboardTags) throw new Error("workspace.import_invalid")
  const tags: string[] = []
  for (const entry of value) {
    if (typeof entry !== "string" || dashboardTagError([...tags, entry])) throw new Error("workspace.import_invalid")
    tags.push(entry)
  }
  return tags
}

/** 看板级查询默认值随导出导入；越界数值按无效文件处理，避免静默降级。 */
export function importQueryDefaults(source: Record<string, unknown>): { max_points?: number; now_delay_seconds?: number } {
  const defaults: { max_points?: number; now_delay_seconds?: number } = {}
  if (source.max_points !== undefined) defaults.max_points = importBoundedInteger(source.max_points, minimumQueryPoints, maximumQueryPoints)
  if (source.now_delay_seconds !== undefined) defaults.now_delay_seconds = importBoundedInteger(source.now_delay_seconds, 0, maximumNowDelaySeconds)
  return defaults
}

export function importBoundedInteger(value: unknown, minimum: number, maximum: number): number {
  const number = finite(value)
  if (number === null || !Number.isInteger(number) || number < minimum || number > maximum) throw new Error("workspace.import_invalid")
  return number
}

export function importVariables(value: unknown[]): DashboardVariable[] {
  if (value.length > limits.variables) throw new Error("workspace.import_invalid")
  const variables: DashboardVariable[] = []
  for (const entry of value) {
    const source = record(entry), name = source?.name, type = source?.type
    const label = source ? text(source.label, limits.variableLabel) : null
    if (!source || typeof name !== "string" || !variablePattern.test(name) || reservedKeys.includes(name)) throw new Error("workspace.import_invalid")
    if (type !== "node" && type !== "group" || !label) throw new Error("workspace.import_invalid")
    if (variables.some((candidate) => candidate.name === name)) throw new Error("workspace.import_invalid")
    variables.push({ name, type, label, ...importVariableOptions(source) })
  }
  return variables
}

/** 选项过滤与多选随看板导入导出：长度或语法不合法的正则让整份文件按无效处理。 */
export function importVariableOptions(source: Record<string, unknown>): Pick<DashboardVariable, "multi" | "include_all" | "regex"> {
  const multi = source.multi === true
  const includeAll = source.include_all === true
  if (includeAll && !multi) throw new Error("workspace.import_invalid")
  if (source.regex === undefined) return { ...(multi ? { multi } : {}), ...(includeAll ? { include_all: true } : {}) }
  const regex = text(source.regex, maximumVariableRegexBytes)
  if (regex === null || variableRegexError(regex)) throw new Error("workspace.import_invalid")
  return { ...(multi ? { multi } : {}), ...(includeAll ? { include_all: true } : {}), ...(regex ? { regex } : {}) }
}

export function importNumber(value: unknown): number {
  const parsed = finite(value)
  if (parsed === null) throw new Error("workspace.import_invalid")
  return parsed
}

export function panelColorValue(value: unknown): PanelColor {
  const color = panelColors.find((candidate) => candidate === value)
  if (!color) throw new Error("workspace.import_invalid")
  return color
}

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export function importTimeToken(value: unknown): string | null {
  if (value === undefined || value === "") return ""
  return text(value, 16)
}

export function importLegendValues(value: unknown): LegendValue[] {
  if (value === undefined) return []
  if (!Array.isArray(value) || value.length > limits.legend) throw new Error("workspace.import_invalid")
  const values: LegendValue[] = []
  for (const entry of value) {
    const kind = legendValueKinds.find((candidate) => candidate === entry)
    if (!kind || values.includes(kind)) throw new Error("workspace.import_invalid")
    values.push(kind)
  }
  return values
}

export function importLegendSort(value: unknown): LegendSort {
  if (value === undefined) return "none"
  const sort = legendSortModes.find((candidate) => candidate === value)
  if (!sort) throw new Error("workspace.import_invalid")
  return sort
}

export function importTooltip(value: unknown): TooltipMode {
  if (value === undefined) return "all"
  const tooltip = tooltipChoices.find((candidate) => candidate === value)
  if (!tooltip) throw new Error("workspace.import_invalid")
  return tooltip
}

export function importPanelType(value: unknown): PanelType {
  const type = panelTypes.find((candidate) => candidate === value)
  if (!type) throw new Error("workspace.import_invalid")
  return type
}

export function importPanelSize(value: unknown): PanelSize {
  const size = panelSizes.find((candidate) => candidate === value)
  if (!size) throw new Error("workspace.import_invalid")
  return size
}

export function importThresholds(value: unknown): DashboardPanel["thresholds"] {
  const list = Array.isArray(value) ? value : []
  if (list.length > limits.thresholds) throw new Error("workspace.import_invalid")
  const thresholds: DashboardPanel["thresholds"] = []
  for (const entry of list) {
    const source = record(entry), threshold = source ? finite(source.value) : null
    const color = panelColors.find((candidate) => candidate === source?.color)
    if (threshold === null || !color) throw new Error("workspace.import_invalid")
    if (thresholds.length > 0 && threshold <= thresholds[thresholds.length - 1].value) throw new Error("workspace.import_invalid")
    thresholds.push({ value: threshold, color })
  }
  return thresholds
}
