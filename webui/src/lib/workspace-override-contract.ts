// 字段覆盖的共享契约：面板切片类型、上限常量、序列类型（单位族）、占位文本与纯校验函数。
// 这一层不依赖任何渲染模块，覆盖解析（workspace-overrides）与覆盖校验（workspace-override-validation）
// 都只单向依赖它，因此不会形成循环引用。
import type { AxisSide } from "./workspace-axis-sides"
import type { DashboardPanel, OverrideMatcherType, OverrideSeriesType } from "@/types/metric-workspace"

/** 渲染器默认的缺失占位文本；面板声明 no_value 时改用面板值。 */
export const defaultNoValue = "—"
/** 与服务端 internal/model 同规则：每个面板最多 16 条覆盖，匹配值 128 字节、单位键 32 字节。 */
export const maximumPanelOverrides = 16
/** 每条覆盖的阈值色带条数上限：与面板级阈值同规则。 */
export const maximumOverrideThresholds = 4
export const maximumOverridePatternBytes = 128
/** 填充目标只按序列名称定位：128 字节与匹配值同一个上限。 */
export const maximumOverrideFillBelowBytes = 128
export const maximumOverrideUnitBytes = 32
/** 与服务端 model.MaxDashboardOverrideDisplayNameBytes 同规则。 */
export const maximumOverrideDisplayNameBytes = 128
/** 与服务端 model.MaxPanelNoValueBytes 同规则。 */
export const maximumPanelNoValueBytes = 32
/** 覆盖匹配方式；与服务端 model.dashboardOverrideMatcherTypes 一一对应。 */
export const overrideMatcherTypeList: OverrideMatcherType[] =
  ["by_name", "by_regex", "by_ref_id", "by_type", "by_value"]
/** 与服务端 model.dashboardSeriesTypeTokens 一一对应的序列类型枚举。 */
export const overrideSeriesTypeList: readonly OverrideSeriesType[] =
  ["none", "number", "percent", "bytes", "rate", "duration", "other"]
/** 与服务端 internal/model 同规则：逐序列阈值只对按单值取色的面板开放。 */
export const overrideThresholdPanelTypes: readonly string[] = ["stat", "gauge", "bargauge", "table", "statetimeline"]
/** 显示名与小数位数只属于会给序列渲染名字与数值的面板。 */
export const overrideStandardPanelTypes: readonly string[] =
  ["timeseries", "barchart", "stat", "gauge", "bargauge", "table", "statetimeline", "piechart"]
/** 逐序列 Min/Max 只属于把单值映射到一段量程的面板。 */
export const overrideLimitPanelTypes: readonly string[] = ["stat", "gauge", "bargauge"]
/** 面板级 No value 只属于会为缺失样本渲染一行文本的面板。 */
export const noValuePanelTypes: readonly string[] = ["stat", "gauge", "bargauge", "table", "statetimeline", "piechart"]

/** 覆盖解析需要的最小面板切片：图表、地理图等子面板都能直接传进来。
 *  type 只在需要判断「该面板类型是否消费这个属性」时提供，缺省表示不做类型门禁。 */
export type OverrideFields = Pick<DashboardPanel, "overrides"> & {
  type?: string
  axis_placement?: AxisSide
  threshold_mode?: string
  thresholds?: DashboardPanel["thresholds"]
  stack?: string
  /** 面板级固定上下限：逐序列柔性上下限与它同用时永远不会生效，校验时需要看到。 */
  min?: number
  max?: number
  /** 面板级小数位数：逐序列 decimals 没有覆盖时沿用它。 */
  decimals?: number
}

/** 取色还需要面板里声明的序列颜色，但那一项可以有条件缺省（例如只传覆盖的调用点）。 */
export type OverridePanelFields = OverrideFields & Partial<Pick<DashboardPanel, "series">>

/**
 * workspaceUnitFamily 把序列的单位归到有限的类型里（Grafana 的 Fields with type）。
 * Beat 的序列值都是数值，单位是它唯一的稳定类型维度：无量纲、百分比、字节、速率、时长与其它。
 * 判定顺序沿用渲染层的单位口径：速率后缀先判，再判字节，避免 bytes_per_second 落进字节族。
 */
export function workspaceUnitFamily(unit: string): OverrideSeriesType {
  if (unit === "") return "none"
  if (unit === "percent") return "percent"
  if (unit.endsWith("_per_second")) return "rate"
  if (unit === "bytes") return "bytes"
  if (unit === "seconds") return "duration"
  if (unit === "count" || unit === "number") return "number"
  return "other"
}

/** workspaceSeriesType 解析一条序列的类型；调用点没带单位时按无量纲处理。 */
export function workspaceSeriesType(series: { unit?: string }): OverrideSeriesType {
  return workspaceUnitFamily(series.unit ?? "")
}

/** workspaceSeriesLatestValue 取序列最新一个有效样本；全为空样本或没有采样点时返回 null。 */
export function workspaceSeriesLatestValue(series: { points?: { value: number | null }[] }): number | null {
  const points = series.points ?? []
  for (let index = points.length - 1; index >= 0; index -= 1) {
    const value = points[index]!.value
    if (value !== null && Number.isFinite(value)) return value
  }
  return null
}

/** overrideSeriesTypeMatches 判定「按字段类型」是否命中：匹配值必须是枚举里的类型。 */
export function overrideSeriesTypeMatches(value: string, series: { unit?: string }): boolean {
  return overrideSeriesTypeList.includes(value as OverrideSeriesType) && workspaceSeriesType(series) === value
}

/** overrideSeriesValueMatches 判定「按值」是否命中：值与序列的最新有效样本精确相等。 */
export function overrideSeriesValueMatches(value: string, series: { points?: { value: number | null }[] }): boolean {
  const expected = Number(value)
  if (value.trim() === "" || !Number.isFinite(expected)) return false
  const actual = workspaceSeriesLatestValue(series)
  return actual !== null && actual === expected
}

/** workspaceNoValue 解析面板的缺失占位文本：空值沿用渲染器默认的破折号。 */
export function workspaceNoValue(panel: { no_value?: string } | undefined): string {
  const value = panel?.no_value
  return value === undefined || value === "" ? defaultNoValue : value
}

/** validOverrideDisplayName 与服务端同规则：可打印、无控制字符、≤128 字节。 */
export function validOverrideDisplayName(value: string): boolean {
  return value.length > 0 && utf8Bytes(value) <= maximumOverrideDisplayNameBytes && !hasControlCharacters(value)
}

/** validNoValue 与服务端同规则：可打印、无控制字符、≤32 字节。 */
export function validNoValue(value: string): boolean {
  return value.length > 0 && utf8Bytes(value) <= maximumPanelNoValueBytes && !hasControlCharacters(value)
}

/** utf8Bytes 按 UTF-8 计算字节数，与服务端的长度口径一致。 */
export function utf8Bytes(value: string): number {
  return new TextEncoder().encode(value).length
}

/** invalidOverrideInteger 判定覆盖里的整数属性是否越界。 */
export function invalidOverrideInteger(value: number, minimum: number, maximum: number): boolean {
  return !Number.isInteger(value) || value < minimum || value > maximum
}

export function hasControlCharacters(value: string): boolean {
  return [...value].some((character) => {
    const code = character.codePointAt(0) ?? 0
    return code < 32 || code === 127
  })
}
