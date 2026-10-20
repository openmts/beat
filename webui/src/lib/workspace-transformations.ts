import { latestWorkspaceValue, seriesLegendStatistics } from "./workspace-chart"
import { panelCalculationValue, panelCalculations } from "./workspace-calculation"
import { sortWorkspaceFrameRows } from "./workspace-table"
import { filterFrameRows, frameCalculationColumn, frameColumnIndex, maximumFrameColumnBytes } from "./workspace-frame-ops"

export { maximumFrameColumnBytes } from "./workspace-frame-ops"
import type {
  DashboardPanel, DashboardTransformation, LegendValue, MetricQueryResult, MetricResultSeries, PanelCalculation,
  TransformationComparison, TransformationGroupCalculation, TransformationGroupField, TransformationKind,
  TransformationOperator, TransformationReduceMode,
} from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：每个面板最多 4 条变换，正则 128 字节、替换文本 120 字节。 */
export const maximumPanelTransformations = 4
export const maximumTransformationPatternBytes = 128
export const maximumTransformationReplacementBytes = 120
export const maximumTransformationLabelLength = 160

export const transformationKinds: TransformationKind[] = ["rename_by_regex", "filter_by_value", "filter_by_label", "sort_by", "limit", "binary_operation", "reduce", "group_by"]
export const transformationOperators: TransformationOperator[] = ["+", "-", "*", "/"]
export const transformationComparisons: TransformationComparison[] = ["gt", "gte", "lt", "lte", "eq", "ne"]
export const transformationOrders = ["asc", "desc"] as const
export const transformationStatistics: LegendValue[] = ["last", "min", "max", "mean", "sum", "count"]
/** group_by 的分组字段：与序列的身份字段一一对应，与服务端集合一致。 */
export const transformationGroupFields: TransformationGroupField[] = ["ref_id", "node_id", "metric", "aggregation"]
/** group_by 的聚合方式：range/delta/percent_change 需要同一条序列的首末样本，跨序列没有定义。 */
export const transformationGroupCalculations: TransformationGroupCalculation[] = ["sum", "mean", "min", "max", "count"]
/** Reduce 模式：与服务端 dashboardTransformationReduceModes 一一对应。 */
export const transformationReduceModes: TransformationReduceMode[] = ["reduce_fields", "series_to_rows"]
/** Reduce → Series to rows 一次最多展开 4 个计算方式，与服务端上限一致。 */
export const maximumTransformationCalculations = 4

export type TransformationFields = Pick<DashboardPanel, "transformations">

/** 结果帧的一个格子：键列给文本，数值列给读数与它所属的序列（值映射、阈值与单位都按它解析）。 */
export interface WorkspaceFrameCell {
  text?: string
  value?: number | null
  series?: MetricResultSeries
  /** 单位覆盖：`count` 这类计算方式数的不是来源单位，格子自己声明单位。 */
  unit?: string
}

/**
 * 结果帧的一列：labelKey 是需要本地化的列名，calculationKey 给出 Grafana 的 `(计算方式)` 后缀，
 * label 是数据自带的列名（例如分组键）。三者按序拼出表头。
 */
export interface WorkspaceFrameColumn {
  key: string
  labelKey?: string
  calculationKey?: string
  label?: string
  value: boolean
}

export interface WorkspaceFrameRow {
  key: string
  cells: WorkspaceFrameCell[]
}

/**
 * 结果帧：Grafana 的 Reduce → Series to rows 与 Group by 输出的不是一组时间序列，而是一张表格
 * （一行一个字段或分组、一列一个计算方式），表格面板照它渲染才能与 Grafana 逐格对齐。
 */
export interface WorkspaceTableFrame {
  kind: "series_to_rows" | "group_by"
  columns: WorkspaceFrameColumn[]
  rows: WorkspaceFrameRow[]
}

export interface PanelTransformationOutcome {
  result: MetricQueryResult
  frame: WorkspaceTableFrame | null
}

/** 变换只重排、重命名或裁剪序列，从不改写采样值，因此可以安全地按顺序组合。 */
export function applyPanelTransformations(panel: TransformationFields, result: MetricQueryResult): MetricQueryResult {
  return applyPanelTransformationsWithFrame(panel, result).result
}

/**
 * 与 applyPanelTransformations 同一趟执行，额外给出链尾的结果帧。帧在链尾成立的情形有三类：
 * 链尾本身就是帧生产者（Reduce → Series to rows、Group by），或者帧产出之后只剩 sort_by/limit
 * 这类同样作用在行上的步骤——Grafana 的变换链就是在一份数据帧上继续做 Sort by 与裁剪。其余种类
 * 会改写序列身份或取值（重命名、按标签过滤、二元运算、再压缩），帧不再代表最终结果，调用方按序列渲染。
 */
export function applyPanelTransformationsWithFrame(panel: TransformationFields, result: MetricQueryResult): PanelTransformationOutcome {
  const chain = panel.transformations ?? []
  if (chain.length === 0) return { result, frame: null }
  let series = result.series
  let frame: WorkspaceTableFrame | null = null
  for (const transformation of chain) {
    const applied = applyTransformationWithFrame(transformation, series, frame)
    series = applied.series
    frame = applied.frame
  }
  return { result: { ...result, series }, frame }
}

/**
 * applyTransformationWithFrame 在帧生产者上重建帧、在排序/裁剪/按列过滤上同步作用到帧的行，其余种类沿用
 * 序列实现。filter_by_value 只有给出 column 时才是行级步骤：没有列选择的历史负载仍旧过滤序列并让帧作废。
 */
function applyTransformationWithFrame(transformation: DashboardTransformation, series: MetricResultSeries[], frame: WorkspaceTableFrame | null): { series: MetricResultSeries[]; frame: WorkspaceTableFrame | null } {
  if (transformation.kind === "reduce") return reduceWithFrame(transformation, series)
  if (transformation.kind === "group_by") return groupByWithFrame(transformation, series)
  if (transformation.kind === "sort_by") return { series: applyTransformation(transformation, series), frame: frame && sortFrame(frame, transformation) }
  if (transformation.kind === "limit") return { series: applyTransformation(transformation, series), frame: frame && limitFrame(frame, transformation) }
  if (transformation.kind === "filter_by_value") return filterByValueWithFrame(transformation, series, frame)
  return { series: applyTransformation(transformation, series), frame: null }
}

/**
 * filterByValueWithFrame 区分两种口径：没有 column 时按序列最新有效样本过滤并让帧作废（历史行为）；
 * 给出 column 时这一步只作用在帧的行上——序列形态保持原样，链上没有帧时它是无操作，避免一条坏配置
 * 把面板清空。
 */
function filterByValueWithFrame(transformation: DashboardTransformation, series: MetricResultSeries[], frame: WorkspaceTableFrame | null): { series: MetricResultSeries[]; frame: WorkspaceTableFrame | null } {
  const column = (transformation.column ?? "").trim()
  if (!column) return { series: applyTransformation(transformation, series), frame: null }
  return { series, frame: frame && filterFrame(frame, transformation, column) }
}

/**
 * filterFrame 按指定列过滤帧的行：只认数值列，列不存在或指向键列时保持原样（与「坏配置不制造二次
 * 损坏」的既有口径一致）；空值不满足任何比较，因此会被丢掉。
 */
function filterFrame(frame: WorkspaceTableFrame, transformation: DashboardTransformation, column: string): WorkspaceTableFrame {
  const index = frameColumnIndex(frame, column)
  if (index < 0 || frame.columns[index]?.value !== true) return frame
  return filterFrameRows(frame, index, (value) => matchesComparison(value, transformation))
}

/** limitFrame 只裁剪行数，与序列形态的 limit 同口径；非法条数在编辑器与服务端已被拦下，这里保持原样。 */
function limitFrame(frame: WorkspaceTableFrame, transformation: DashboardTransformation): WorkspaceTableFrame {
  if (transformation.count === undefined) return frame
  return { ...frame, rows: frame.rows.slice(0, Math.max(0, transformation.count)) }
}

/**
 * sortFrame 按 Grafana 的 Sort by 口径重排帧的行：给出 `field` 时按该列排序（含分组键这类文本列），
 * 否则排序依据取自 `statistic` 对应的计算列；没有匹配列时保持数据原本的顺序。比较规则与表格面板
 * 交互式表头排序共用一份实现：键列比文本、数值列比数值、空值恒排最后。
 */
function sortFrame(frame: WorkspaceTableFrame, transformation: DashboardTransformation): WorkspaceTableFrame {
  const field = (transformation.field ?? "").trim()
  const index = field ? frameColumnIndex(frame, field) : frameCalculationColumn(frame, transformation.statistic ?? "last")
  if (index < 0) return frame
  const direction = transformation.order === "desc" ? "desc" : "asc"
  return { ...frame, rows: sortWorkspaceFrameRows(frame.rows, frame.columns, index, direction) }
}

function applyTransformation(transformation: DashboardTransformation, series: MetricResultSeries[]): MetricResultSeries[] {
  if (transformation.kind === "rename_by_regex") return renameTransformation(transformation, series)
  if (transformation.kind === "filter_by_value") return series.filter((value) => matchesComparison(latestWorkspaceValue(value), transformation))
  if (transformation.kind === "filter_by_label") return labelTransformation(transformation, series)
  if (transformation.kind === "sort_by") return sortTransformation(transformation, series)
  if (transformation.kind === "binary_operation") return binaryTransformation(transformation, series)
  // limit：只裁剪条数，需要「最大的三条」时把 sort_by 放在前面。
  if (transformation.count === undefined) return series
  return series.slice(0, Math.max(0, transformation.count))
}

// 标签过滤：命中正则的序列保留；Invert 时反过来保留未命中的序列，坏正则同样保持原样。
function labelTransformation(transformation: DashboardTransformation, series: MetricResultSeries[]): MetricResultSeries[] {
  const pattern = transformation.pattern
  if (!pattern) return series
  let expression: RegExp
  try {
    expression = new RegExp(pattern)
  } catch (_) {
    void _
    return series
  }
  return series.filter((value) => expression.test(value.label) !== (transformation.invert === true))
}

/** 无效正则视为不生效：编辑器与服务端都会先拦下，渲染期不再制造二次损坏。 */
function renameTransformation(transformation: DashboardTransformation, series: MetricResultSeries[]): MetricResultSeries[] {
  const pattern = transformation.pattern
  if (!pattern) return series
  let expression: RegExp
  try {
    expression = new RegExp(pattern, "g")
  } catch (_) {
    void _
    return series
  }
  const replacement = transformation.replacement ?? ""
  return series.map((value) => ({ ...value, label: value.label.replace(expression, replacement).slice(0, maximumTransformationLabelLength) }))
}

/** 未知比较符号保持原样，避免一条坏配置把面板清空。 */
function matchesComparison(value: number | null, transformation: DashboardTransformation): boolean {
  const threshold = transformation.value
  if (transformation.comparison === undefined || !transformationComparisons.includes(transformation.comparison)) return true
  if (value === null || threshold === undefined || !Number.isFinite(threshold)) return false
  switch (transformation.comparison) {
    case "gt": return value > threshold
    case "gte": return value >= threshold
    case "lt": return value < threshold
    case "lte": return value <= threshold
    case "eq": return value === threshold
    default: return value !== threshold
  }
}

/** 缺失样本始终排在最后，与图例排序保持一致；只指定了结果帧列时这一步不动序列。 */
function sortTransformation(transformation: DashboardTransformation, series: MetricResultSeries[]): MetricResultSeries[] {
  const statistic = transformation.statistic
  if (statistic === undefined || !transformationStatistics.includes(statistic)) return series
  const ranked = series.map((value) => ({ value, statistic: seriesLegendStatistics(value)[statistic] }))
  ranked.sort((left, right) => {
    if (left.statistic === null || right.statistic === null) {
      return left.statistic === right.statistic ? 0 : left.statistic === null ? 1 : -1
    }
    return transformation.order === "desc" ? right.statistic - left.statistic : left.statistic - right.statistic
  })
  return ranked.map((entry) => entry.value)
}

export function emptyTransformation(kind: TransformationKind = "filter_by_value"): DashboardTransformation {
  if (kind === "rename_by_regex") return { kind, pattern: "^(.*)$", replacement: "$1" }
  if (kind === "filter_by_label") return { kind, pattern: "^node-" }
  if (kind === "sort_by") return { kind, statistic: "last", order: "desc" }
  if (kind === "limit") return { kind, count: 5 }
  if (kind === "binary_operation") return { kind, left: "A", operator: "+", right: "B" }
  if (kind === "reduce") return { kind, mode: "reduce_fields", calculations: ["mean"] }
  if (kind === "group_by") return { kind, group_by: "metric", calculations: ["sum"] }
  return { kind, comparison: "gt", value: 0 }
}

/** 切换种类时整体换掉参数，避免把上一组字段带进新负载被服务端拒绝。 */
export function switchTransformationKind(kind: string): DashboardTransformation {
  return emptyTransformation(transformationKinds.includes(kind as TransformationKind) ? kind as TransformationKind : "filter_by_value")
}

/** transformationError 返回目录键，null 表示可以保存；规则与服务端 ValidateDashboard 一致。 */
export function transformationError(panel: TransformationFields): string | null {
  const chain = panel.transformations ?? []
  if (chain.length > maximumPanelTransformations) return "workspace.transformations_too_many"
  for (const transformation of chain) {
    if (transformation.kind === "rename_by_regex") {
      if (!transformation.pattern || new TextEncoder().encode(transformation.pattern).length > maximumTransformationPatternBytes || !isValidExpression(transformation.pattern)) {
        return "workspace.transformation_pattern_invalid"
      }
      if (new TextEncoder().encode(transformation.replacement ?? "").length > maximumTransformationReplacementBytes || hasControlCharacters(transformation.replacement ?? "")) {
        return "workspace.transformation_replacement_invalid"
      }
      continue
    }
    if (transformation.kind === "filter_by_value") {
      if (!transformation.comparison || !transformationComparisons.includes(transformation.comparison)) return "workspace.transformation_comparison_invalid"
      if (transformation.value === undefined || !Number.isFinite(transformation.value)) return "workspace.transformation_value_invalid"
      if (!validTransformationColumn(transformation.column)) return "workspace.transformation_column_invalid"
      continue
    }
    if (transformation.kind === "filter_by_label") {
      if (!transformation.pattern || new TextEncoder().encode(transformation.pattern).length > maximumTransformationPatternBytes || !isValidExpression(transformation.pattern)) {
        return "workspace.transformation_pattern_invalid"
      }
      continue
    }
    if (transformation.kind === "sort_by") {
      const statistic = transformation.statistic ?? ""
      if (statistic && !transformationStatistics.includes(statistic)) return "workspace.transformation_statistic_invalid"
      if (!statistic && !transformation.field) return "workspace.transformation_statistic_invalid"
      if (!validTransformationColumn(transformation.field)) return "workspace.transformation_field_invalid"
      if (!transformationOrders.includes(transformation.order as "asc" | "desc")) return "workspace.transformation_order_invalid"
      continue
    }
    if (transformation.kind === "limit") {
      if (transformation.count === undefined || !Number.isInteger(transformation.count) || transformation.count < 1 || transformation.count > 8) {
        return "workspace.transformation_count_invalid"
      }
      continue
    }
    if (transformation.kind === "reduce") {
      const calculations = reduceCalculations(transformation)
      if (calculations === null) return "workspace.transformation_calculation_invalid"
      if (transformation.calculation && transformation.calculations) return "workspace.transformation_calculation_invalid"
      if (calculations.length > maximumTransformationCalculations) return "workspace.transformation_calculations_invalid"
      if (new Set(calculations).size !== calculations.length) return "workspace.transformation_calculations_invalid"
      if (transformation.mode !== undefined && !transformationReduceModes.includes(transformation.mode)) return "workspace.transformation_reduce_mode_invalid"
      if (calculations.length > 1 && transformation.mode !== "series_to_rows") return "workspace.transformation_calculations_invalid"
      continue
    }
    if (transformation.kind === "group_by") {
      if (!transformation.group_by || !transformationGroupFields.includes(transformation.group_by)) return "workspace.transformation_group_by_invalid"
      if (transformation.calculation && transformation.calculations) return "workspace.transformation_calculations_invalid"
      const raw = transformation.calculations ?? (transformation.calculation ? [transformation.calculation] : [])
      if (raw.length === 0) return "workspace.transformation_calculation_invalid"
      if (raw.length > maximumTransformationCalculations) return "workspace.transformation_calculations_invalid"
      const calculations = raw.filter((value) => transformationGroupCalculations.includes(value as TransformationGroupCalculation))
      if (calculations.length !== raw.length) return "workspace.transformation_calculation_invalid"
      if (new Set(calculations).size !== calculations.length) return "workspace.transformation_calculations_invalid"
      const alias = transformation.alias ?? ""
      if (new TextEncoder().encode(alias).length > maximumTransformationReplacementBytes || hasControlCharacters(alias)) return "workspace.transformation_alias_invalid"
      continue
    }
    if (transformation.kind === "binary_operation") {
      if (!isTransformationSelector(transformation.left ?? "") || !isTransformationSelector(transformation.right ?? "")) return "workspace.transformation_selector_invalid"
      if (!transformation.operator || !transformationOperators.includes(transformation.operator)) return "workspace.transformation_operator_invalid"
      const alias = transformation.alias ?? ""
      if (new TextEncoder().encode(alias).length > maximumTransformationReplacementBytes || hasControlCharacters(alias)) return "workspace.transformation_alias_invalid"
      continue
    }
    return "workspace.transformation_kind_invalid"
  }
  return null
}

/**
 * validTransformationColumn 校验列选择器（filter_by_value 的 column、sort_by 的 field）：可选的 64 字节
 * 可打印文本，空值与缺省同义；规则与服务端 validateTransformationColumn 一致。
 */
function validTransformationColumn(value: string | undefined): boolean {
  if (value === undefined || value === "") return true
  return new TextEncoder().encode(value).length <= maximumFrameColumnBytes && !hasControlCharacters(value)
}

/**
 * groupByCalculations 兼容历史单个 calculation 与新的 calculations 多选：两者同时出现、一个都没有、
 * 非本种类的值、重复值或超过上限都返回 null，与服务端 transformationCalculationList 同规则。
 */
export function groupByCalculations(transformation: DashboardTransformation): TransformationGroupCalculation[] | null {
  if (transformation.calculation && transformation.calculations) return null
  const raw = transformation.calculations ?? (transformation.calculation ? [transformation.calculation] : [])
  if (raw.length === 0 || raw.length > maximumTransformationCalculations) return null
  const calculations = raw.filter((value): value is TransformationGroupCalculation =>
    transformationGroupCalculations.includes(value as TransformationGroupCalculation))
  return calculations.length === raw.length && new Set(calculations).size === calculations.length ? calculations : null
}

/**
/**
 * Group by：按序列的身份字段分组（Grafana 的 Transform → Group by）。这一步同时产出两种形态：
 * 序列形态按时间点聚合，组内多条序列合成一条时间序列，可以照旧画在折线、柱状等面板上；帧形态按
 * Grafana 的表格口径聚合整个窗口，一行一个分组、一列一个 `<分组字段> (计算方式)`，交给表格面板。
 * 与 Grafana 一样可以选多个计算方式，顺序与声明一致；组内没有有效样本时该格留空，绝不伪造 0。
 */
function groupByWithFrame(transformation: DashboardTransformation, series: MetricResultSeries[]): { series: MetricResultSeries[]; frame: WorkspaceTableFrame | null } {
  const field = transformationGroupFields.find((value) => value === transformation.group_by)
  const calculations = groupByCalculations(transformation)
  if (!field || !calculations) return { series, frame: null }
  const groups = new Map<string, MetricResultSeries[]>()
  for (const value of series) {
    const members = groups.get(value[field])
    if (members) members.push(value)
    else groups.set(value[field], [value])
  }
  const entries = [...groups.entries()]
  return { series: groupBySeries(entries, calculations, transformation), frame: groupByFrame(entries, calculations, transformation, field) }
}

/** groupBySeries 是历史的序列形态：每个计算方式各生成一条按时间点聚合的合并序列。 */
function groupBySeries(entries: [string, MetricResultSeries[]][], calculations: TransformationGroupCalculation[], transformation: DashboardTransformation): MetricResultSeries[] {
  const multiple = calculations.length > 1, base = transformation.alias ?? ""
  return entries.flatMap(([key, members], index) => calculations.map((calculation) => {
    const points = groupSeriesPoints(members, calculation)
    return { ...members[0], ref_id: groupByRefID(index, calculation, multiple),
      label: groupBySeriesLabel(base || key, calculation, multiple), state: points.length ? "ok" : "empty", points }
  }))
}

/** groupByFrame 是 Grafana 的表格口径：一行一个分组，列名是 `<分组字段> (计算方式)`，值聚合整个窗口。 */
function groupByFrame(entries: [string, MetricResultSeries[]][], calculations: TransformationGroupCalculation[], transformation: DashboardTransformation, field: TransformationGroupField): WorkspaceTableFrame {
  const labelKey = `workspace.transformation_group_field_${field}`
  const columns: WorkspaceFrameColumn[] = [{ key: `group_${field}`, labelKey, value: false },
    ...calculations.map((calculation) => ({ key: `calculation_${calculation}`, labelKey, calculationKey: `workspace.calculation_${calculation}`, value: true }))]
  const alias = transformation.alias ?? ""
  const rows = entries.map(([key, members], index) => ({
    key: `group_${index}`,
    cells: [{ text: alias || key }, ...calculations.map((calculation) => frameValueCell(members[0], groupWindowValue(members, calculation), calculation))],
  }))
  return { kind: "group_by", columns, rows }
}

/** frameValueCell 组装一个数值格：带上来源序列用于取色，计数类计算方式改用 count 单位。 */
function frameValueCell(series: MetricResultSeries, value: number | null, calculation: string): WorkspaceFrameCell {
  return calculation === "count" ? { value, series, unit: "count" } : { value, series }
}

/** groupWindowValue 按 Grafana 的 Group by 口径聚合整个窗口：count 数的是有效样本数，空组留空。 */
function groupWindowValue(members: MetricResultSeries[], calculation: TransformationGroupCalculation): number | null {
  const values: number[] = []
  for (const member of members) {
    for (const point of member.points) {
      if (point.value !== null && Number.isFinite(point.value)) values.push(point.value)
    }
  }
  return values.length === 0 ? null : aggregateGroupValues(values, calculation)
}

/** groupByRefID 保留历史单计算写的 group_by_<组号>，多计算时补上计算方式避免行之间互相覆盖。 */
function groupByRefID(index: number, calculation: TransformationGroupCalculation, multiple: boolean): string {
  return multiple ? `group_by_${index}_${calculation}` : `group_by_${index}`
}

/** groupBySeriesLabel 与 Grafana 的输出列命名同口径：多个计算方式时在名字后追加计算方式。 */
function groupBySeriesLabel(base: string, calculation: TransformationGroupCalculation, multiple: boolean): string {
  return (multiple ? `${base} (${calculation})` : base).slice(0, maximumTransformationLabelLength)
}

/** groupSeriesPoints 单次遍历收集每个时间点的有效样本，再按聚合方式生成合并后的采样点。 */
function groupSeriesPoints(members: MetricResultSeries[], calculation: TransformationGroupCalculation): { timestamp: number; value: number }[] {
  const samples = new Map<number, number[]>()
  for (const member of members) {
    for (const point of member.points) {
      if (point.value === null || !Number.isFinite(point.value)) continue
      const bucket = samples.get(point.timestamp)
      if (bucket) bucket.push(point.value)
      else samples.set(point.timestamp, [point.value])
    }
  }
  return [...samples.entries()].sort(([left], [right]) => left - right)
    .map(([timestamp, values]) => ({ timestamp, value: aggregateGroupValues(values, calculation) }))
}

/** aggregateGroupValues 只对有效样本求值：count 数的是有效样本数，因此不会把空桶算成 0。 */
function aggregateGroupValues(values: number[], calculation: TransformationGroupCalculation): number {
  if (calculation === "count") return values.length
  if (calculation === "min") return Math.min(...values)
  if (calculation === "max") return Math.max(...values)
  const total = values.reduce((sum, value) => sum + value, 0)
  return calculation === "sum" ? total : total / values.length
}

/**
 * reduceCalculations 统一两种写法：历史看板用单个 calculation，新看板用 calculations 多选。
 * 两者同时出现或一个都没有都返回 null，与服务端 reduceTransformationCalculations 同规则。
 */
export function reduceCalculations(transformation: DashboardTransformation): PanelCalculation[] | null {
  if (transformation.calculation && transformation.calculations) return null
  const raw = transformation.calculations ?? (transformation.calculation ? [transformation.calculation] : [])
  if (raw.length === 0) return null
  const calculations = raw.filter((value): value is PanelCalculation => panelCalculations.includes(value))
  return calculations.length === raw.length ? calculations : null
}

/**
 * Reduce：把每条序列按窗口压成结果（Grafana 的 Transform → Reduce）。取值方式与面板级
 * Calculation 共用一份实现，压出来的样本落在该序列最后一个有效采样的时间戳上；没有任何有效
 * 采样时保留空样本，让渲染层继续走「无数据」而不是伪造一个 0。
 *
 * 默认模式 reduce_fields 每条序列压成一个样本；series_to_rows 对这种形态的最终结果是 Grafana
 * 的表格帧——一行一个字段、一列一个计算方式，因此额外产出一份帧交给表格面板（序列仍然产出，
 * 折线等面板照旧按它渲染）。
 */
function reduceWithFrame(transformation: DashboardTransformation, series: MetricResultSeries[]): { series: MetricResultSeries[]; frame: WorkspaceTableFrame | null } {
  const calculations = reduceCalculations(transformation)
  if (!calculations) return { series, frame: null }
  if (transformation.mode === "series_to_rows") return { series: reduceSeriesToRows(series, calculations), frame: reduceFrame(series, calculations) }
  return { series: reduceFields(series, calculations[0]), frame: null }
}

/** reduceFields 是历史的默认模式：每条序列压成一个样本。 */
function reduceFields(series: MetricResultSeries[], calculation: PanelCalculation): MetricResultSeries[] {
  return series.map((value) => {
    const reduced = panelCalculationValue(value, calculation)
    const timestamp = lastFiniteTimestamp(value)
    return { ...value, state: reduced === null ? "empty" : "ok", points: reduced === null || timestamp === null ? [] : [{ timestamp, value: reduced }] }
  })
}

/** reduceFrame 按 Grafana 的 Series to rows：一行一个字段，一列一个计算方式（列名就是计算方式）。 */
function reduceFrame(series: MetricResultSeries[], calculations: PanelCalculation[]): WorkspaceTableFrame {
  const columns: WorkspaceFrameColumn[] = [{ key: "field", labelKey: "workspace.frame_field", value: false },
    ...calculations.map((calculation) => ({ key: `calculation_${calculation}`, labelKey: `workspace.calculation_${calculation}`, value: true }))]
  const rows = series.map((value, index) => ({
    key: `field_${index}`,
    cells: [{ text: value.label }, ...calculations.map((calculation) => frameValueCell(value, panelCalculationValue(value, calculation), calculation))],
  }))
  return { kind: "series_to_rows", columns, rows }
}

/** reduceSeriesToRows 按「序列 × 计算方式」展开；ref_id 与行标签都带上计算方式，行之间不互相覆盖。 */
function reduceSeriesToRows(series: MetricResultSeries[], calculations: PanelCalculation[]): MetricResultSeries[] {
  const rows: MetricResultSeries[] = []
  series.forEach((value, seriesIndex) => {
    const timestamp = lastFiniteTimestamp(value)
    calculations.forEach((calculation) => {
      const reduced = panelCalculationValue(value, calculation)
      rows.push({
        ...value,
        ref_id: `reduce_${seriesIndex}_${calculation}`,
        label: `${value.label} · ${calculation}`.slice(0, maximumTransformationLabelLength),
        state: reduced === null ? "empty" : "ok",
        points: reduced === null || timestamp === null ? [] : [{ timestamp, value: reduced }],
      })
    })
  })
  return rows
}

/** lastFiniteTimestamp 取最后一个有效采样的时间戳；整条序列都是空桶时回落到最后一个采样点。 */
function lastFiniteTimestamp(series: MetricResultSeries): number | null {
  let finite: number | null = null, fallback: number | null = null
  for (const point of series.points) {
    fallback = point.timestamp
    if (point.value !== null && Number.isFinite(point.value)) finite = point.timestamp
  }
  return finite ?? fallback
}

/**
 * 二元运算：按时间戳对齐左右两条序列做四则运算，结果作为一条新序列追加在末尾。
 * 选择器是序列标签的精确匹配，写成 /re/ 时按正则取第一条命中；任一侧缺样本、时间戳对不上或
 * 除数为零的样本直接跳过，不把 NaN/Infinity 交给渲染层。
 */
function binaryTransformation(transformation: DashboardTransformation, series: MetricResultSeries[]): MetricResultSeries[] {
  const operator = transformation.operator
  const left = findSelectorSeries(series, transformation.left)
  const right = findSelectorSeries(series, transformation.right)
  if (!left || !right || operator === undefined || !transformationOperators.includes(operator)) return series
  const points = binaryPoints(left, right, operator)
  const label = (transformation.alias || `${left.label} ${operator} ${right.label}`).slice(0, maximumTransformationLabelLength)
  return [...series, {
    ...left, ref_id: `binary_operation_${series.length}`, label, state: points.length ? "ok" : "empty", points,
  }]
}

function findSelectorSeries(series: MetricResultSeries[], selector: string | undefined): MetricResultSeries | undefined {
  if (!selector) return undefined
  const pattern = selectorPattern(selector)
  if (pattern === null) return series.find((value) => value.label === selector)
  let expression: RegExp
  try {
    expression = new RegExp(pattern)
  } catch (_) {
    void _
    return undefined
  }
  return series.find((value) => expression.test(value.label))
}

function binaryPoints(left: MetricResultSeries, right: MetricResultSeries, operator: TransformationOperator): { timestamp: number; value: number | null }[] {
  const samples = new Map<number, number>()
  for (const point of right.points) if (point.value !== null && Number.isFinite(point.value)) samples.set(point.timestamp, point.value)
  const points: { timestamp: number; value: number | null }[] = []
  for (const point of left.points) {
    if (point.value === null || !Number.isFinite(point.value)) continue
    const other = samples.get(point.timestamp)
    if (other === undefined) continue
    const value = binaryValue(point.value, other, operator)
    if (value !== null) points.push({ timestamp: point.timestamp, value })
  }
  return points
}

function binaryValue(left: number, right: number, operator: TransformationOperator): number | null {
  if (operator === "+") return left + right
  if (operator === "-") return left - right
  if (operator === "*") return left * right
  return right === 0 ? null : left / right
}

/** 选择器只接受标签文本或 /re/ 形式的 RE2 正则，长度与可打印性同其它变换字段。 */
export function isTransformationSelector(selector: string): boolean {
  if (!selector || new TextEncoder().encode(selector).length > maximumTransformationPatternBytes || hasControlCharacters(selector)) return false
  const pattern = selectorPattern(selector)
  return pattern === null || isValidExpression(pattern)
}

/** 只在两端都是斜杠且长度足够时按正则解释，否则整串都是字面标签。 */
function selectorPattern(selector: string): string | null {
  if (selector.length < 3 || !selector.startsWith("/") || !selector.endsWith("/")) return null
  const pattern = selector.slice(1, -1)
  return pattern === "" ? null : pattern
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

function hasControlCharacters(value: string): boolean {
  return [...value].some((character) => { const code = character.codePointAt(0) ?? 0; return code < 32 || code === 127 })
}
