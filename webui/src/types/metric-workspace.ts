export type MetricAggregation = "avg" | "min" | "max" | "sum" | "last" | "rate"
export type PanelColor = "blue" | "green" | "amber" | "rose" | "violet" | "cyan" | "slate"
export type PanelType = "timeseries" | "barchart" | "stat" | "gauge" | "table" | "bargauge" | "piechart" | "heatmap" | "histogram" | "statetimeline" | "geomap" | "text" | "row"
export type HeatmapScale = "linear" | "log"
/** 直方图计数轴刻度：linear 等分柱高，log 压高低频差异。 */
export type HistogramScale = "linear" | "log"
/** Geo map 图层：标记点或热力网格。 */
export type GeoLayer = "markers" | "heatmap"
/** Geo map 的地点表条目：把序列匹配到的节点/设备映射到经纬度。 */
export interface DashboardGeoLocation {
  match: string
  label?: string
  latitude: number
  longitude: number
}
/** 状态时间线的取值标签模式：auto 只在色块放得下时显示取值。 */
export type TimelineShowValues = "auto" | "always" | "never"
export type BarOrientation = "auto" | "horizontal" | "vertical"
export type BarDisplay = "basic" | "gradient" | "lcd"
/** Grafana 的 Bar alignment：auto 是缺省（等同于 centered）。 */
export type BarAlignment = "auto" | "before" | "centered" | "after"
export type PieChartType = "pie" | "donut"
export type PieLabelMode = "percent" | "value" | "both"
export type PanelSize = "small" | "medium" | "large"
export type LegendValue = "last" | "min" | "max" | "mean" | "sum" | "count"
export type LegendSort = "none" | "asc" | "desc"
export type TooltipMode = "all" | "single" | "off"
/** 阈值显示模式：off/line/area/background；缺省等于 line。 */
export type ThresholdDisplayMode = "off" | "line" | "area" | "background"
/** Grafana 风格的面板数据链接：标题是按钮文案，URL 支持 ${...} 占位符。 */
export interface DashboardPanelLink {
  title: string
  url: string
  /** 是否在新标签页打开；缺省在当前标签页内导航。 */
  new_tab?: boolean
  /** 按钮左侧图标；缺省是 external。服务端只接受固定枚举。 */
  icon?: DashboardLinkIcon
}

/** 表格单元格显示模式（Grafana 的 Cell display mode），与服务端 model.DashboardCellDisplayModes 一一对应。 */
export const cellDisplayModes = ["text", "color_text", "color_background", "gradient"] as const
export type DashboardCellDisplayMode = typeof cellDisplayModes[number]

/** 表格形态（Grafana 的 Table 布局），与服务端 model.DashboardTableOrientations 一一对应：long 是
 *  历史默认的一行一个采样；wide 是 Grafana 默认的宽表——一列一条序列、一行一个时间点。 */
export const tableOrientations = ["long", "wide"] as const
export type DashboardTableOrientation = typeof tableOrientations[number]

/** 链接按钮图标集合，与服务端 model.DashboardLinkIcons 一一对应。 */
export const dashboardLinkIcons = ["external", "dashboard", "alert", "docs", "info", "link"] as const
export type DashboardLinkIcon = typeof dashboardLinkIcons[number]

/** 面板值计算：把一条序列压成一个数值；缺省等于 last（最新有效样本）。
 *  range/delta/percent_change 与 Grafana 的 Range/Delta/Percent change 同义，窗口内有效样本不足两个时为空。 */
export type PanelCalculation = "last" | "first" | "min" | "max" | "mean" | "sum" | "count" | "range" | "delta" | "percent_change"

export interface MetricDefinition {
  key: string
  label: string
  description: string
  source: "builtin" | "derived" | "custom"
  kind: "gauge" | "counter"
  unit: string
  aggregations: MetricAggregation[]
  default_aggregation: MetricAggregation
  public: boolean
  enabled: boolean
  retention_days: number
  retention_override_days: number | null
  revision: number
}

export interface MetricCatalog {
  version: number
  revision: number
  metrics: MetricDefinition[]
}

export interface MetricQuerySeries {
  device_id?: string
  ref_id: string
  node_id: string
  metric: string
  aggregation: MetricAggregation
}

export interface MetricQuery {
  from: string
  to: string
  max_points: number
  series: MetricQuerySeries[]
}

export interface MetricLatestQuerySeries {
  ref_id: string
  node_id: string
  metric: string
}

/** 即时查询不带时间范围：服务端返回每条序列当前的最新原始样本。 */
export interface MetricLatestQuery {
  series: MetricLatestQuerySeries[]
}

export interface MetricLatestResultSeries extends MetricLatestQuerySeries {
  label: string
  unit: string
  state: "ok" | "empty"
  value?: number
  sample_at?: string
}

export interface MetricLatestResult {
  series: MetricLatestResultSeries[]
}

export interface MetricQueryResult {
  from: string
  to: string
  interval_ms: number
  series: MetricResultSeries[]
}

export interface MetricResultSeries extends MetricQuerySeries {
  label: string
  unit: string
  available_from: string | null
  state: "ok" | "empty"
  points: { timestamp: number; value: number | null }[]
}

export interface DashboardVariable {
  name: string
  type: "node" | "group"
  label: string
  /** 允许同时选择多个选项；缺省为单选，保持既有看板行为。 */
  multi?: boolean
  /** 提供 Grafana 风格的 All 选项；只能与 multi 同用。 */
  include_all?: boolean
  /** 按 RE2 语义过滤可选项；空值表示不过滤。 */
  regex?: string
}

/** 变量当前选择：每个变量保存一个有序的值列表，单选就是长度为 1 的列表。 */
export type WorkspaceSelections = Record<string, string[]>

export interface DashboardSeries extends MetricQuerySeries {
  label: string
  color: PanelColor
}

export type ValueMappingType = "value" | "range" | "special"

export type TransformationKind = "rename_by_regex" | "filter_by_value" | "filter_by_label" | "sort_by" | "limit" | "binary_operation" | "reduce" | "group_by"
export type TransformationComparison = "gt" | "gte" | "lt" | "lte" | "eq" | "ne"
export type TransformationOrder = "asc" | "desc"
/** 二元运算的运算符，与服务端四则运算集合一致。 */
export type TransformationOperator = "+" | "-" | "*" | "/"
/** group_by 的分组字段：与序列的身份字段一一对应。 */
export type TransformationGroupField = "ref_id" | "node_id" | "metric" | "aggregation"
/** group_by 的聚合方式：只保留跨序列有意义的取值，与服务端集合一致。 */
export type TransformationGroupCalculation = "sum" | "mean" | "min" | "max" | "count"
/** Reduce 模式：reduce_fields 每条序列一个结果，series_to_rows 展开成「序列 × 计算方式」长表行。 */
export type TransformationReduceMode = "reduce_fields" | "series_to_rows"

/**
 * Grafana 风格的面板级结果变换：按数组顺序作用于查询结果。
 * 字段按 kind 使用，不属于该种类的字段留空；服务端 `ValidateDashboard` 会拒绝混用的负载。
 */
export interface DashboardTransformation {
  kind: TransformationKind
  /** rename_by_regex：RE2 正则与替换文本。 */
  pattern?: string
  replacement?: string
  /** filter_by_value：按序列最新有效样本与阈值比较；给出 column 时改为按结果帧的该列过滤行。 */
  comparison?: TransformationComparison
  value?: number
  column?: string
  /** filter_by_label：按序列标签匹配 Pattern，Invert 反转命中集合。 */
  invert?: boolean
  /** sort_by：按图例统计量排序（也可直接用 field 指定结果帧的列）；limit 保留前 N 条序列。 */
  statistic?: LegendValue
  order?: TransformationOrder
  count?: number
  field?: string
  /** binary_operation：按时间戳对齐左右两条序列做四则运算，结果作为新序列追加在末尾。 */
  left?: string
  operator?: TransformationOperator
  right?: string
  alias?: string
  /** reduce：把每条序列按窗口压成结果；取值集合与面板级 Calculation 相同。
   *  历史看板写单个 calculation；新看板可用 calculations 多选，此时 mode 必须是 series_to_rows。 */
  calculation?: PanelCalculation
  calculations?: PanelCalculation[]
  mode?: TransformationReduceMode
  /** group_by：按这个身份字段分组，再用 calculation 把组内多条序列按时间点聚合成一条。 */
  group_by?: TransformationGroupField
}

/** 字段覆盖的匹配方式，与服务端 model.dashboardOverrideMatcherTypes 一一对应。 */
export const overrideMatcherTypes = ["by_name", "by_regex", "by_ref_id", "by_type", "by_value"] as const
export type OverrideMatcherType = typeof overrideMatcherTypes[number]

export interface DashboardOverrideMatcher {
  type: OverrideMatcherType
  /** by_name 是序列标签精确匹配，by_regex 是 RE2 语义的正则，by_ref_id 精确匹配查询序列 id；
   *  by_type 匹配序列的单位族，by_value 匹配最新样本等于该数值的序列。 */
  value: string
}

/** 字段覆盖的序列类型（Grafana 的 Fields with type）：Beat 的序列值都是数值，
 *  单位族是它唯一的稳定类型维度，与服务端 model.dashboardSeriesTypeTokens 一一对应。 */
export const overrideSeriesTypes = ["none", "number", "percent", "bytes", "rate", "duration", "other"] as const
export type OverrideSeriesType = typeof overrideSeriesTypes[number]

/**
 * Grafana 的 Overrides 标签页：命中匹配器的序列按覆盖属性渲染。
 * 覆盖只改显示，不改采样值；多条覆盖按数组顺序叠加，后面的覆盖赢。
 */
export interface DashboardOverride {
  matcher: DashboardOverrideMatcher
  /** 命中序列的调色板键；空值表示沿用序列自身的颜色。 */
  color?: PanelColor
  /** 命中序列的显示单位键；空值表示沿用查询结果里的单位。 */
  unit?: string
  /** 把命中序列从图例里隐藏；面板仍然绘制该序列。 */
  hide_legend?: boolean
  /** 命中序列的折线宽度（1..4）；空值表示沿用面板线宽。只有 timeseries 面板消费它。 */
  line_width?: number
  /** 命中序列的填充不透明度（0..100，百分比）；空值表示沿用面板填充。只有 timeseries/barchart 消费它。 */
  fill_opacity?: number
  /** 命中序列画在哪根值轴上（Grafana 的 Overrides → Axis → Placement）；空值表示沿用面板轴位置。 */
  axis_placement?: "left" | "right"
  /** 命中序列自己的一套阈值色带（Grafana 的 Overrides → Thresholds）；空值表示沿用面板阈值。 */
  thresholds?: { value: number; color: PanelColor }[]
  /** 另一条序列的名称：命中序列的填充区延伸到它（Grafana 的 Overrides → Graph styles → Fill below to）。 */
  fill_below_to?: string
  /** 命中序列所在值轴的标题（Grafana 的 Overrides → Axis → Label）；空值表示沿用面板级标题。 */
  axis_label?: string
  /** 命中序列所在值轴的柔性上下限（Grafana 的 Overrides → Axis → Soft min/Soft max）。 */
  axis_soft_min?: number
  axis_soft_max?: number
  /** 命中序列所在值轴的宽度（Grafana 的 Overrides → Axis → Width）；0 与缺省一样表示自动。 */
  axis_width?: number
  /** 命中序列的显示名（Grafana 的 Overrides → Standard options → Display name）；空值沿用序列标签。 */
  display_name?: string
  /** 命中序列的小数位数（Grafana 的 Overrides → Standard options → Decimals，0..6）；空值沿用面板级。 */
  decimals?: number
  /** 命中序列的值域（Grafana 的 Overrides → Standard options → Min / Max）；只有 stat/gauge/bargauge 消费。 */
  min?: number
  max?: number
}

/** Grafana 风格的值映射：第一条命中的映射生效，未命中时仍按数值与阈值渲染。 */
export interface DashboardValueMapping {
  type: ValueMappingType
  value?: number
  from?: number
  to?: number
  /** 特殊值匹配；当前只支持空样本。 */
  match?: "null"
  text?: string
  color?: PanelColor
}

export interface DashboardPanel {
  id: string
  title: string
  type: PanelType
  size: PanelSize
  /** 按变量逐节点重复渲染；空值表示单面板。 */
  repeat?: string
  /** Grafana 风格的面板数据链接（钻取入口）；空值表示该面板没有链接。 */
  links?: DashboardPanelLink[]
  /** 文本面板的 Markdown 内容；只对 text 面板有效，其余类型必须留空。 */
  text?: string
  legend: boolean
  legend_values?: LegendValue[]
  legend_sort?: LegendSort
  /** Grafana 的 Legend placement：bottom（缺省）/right；只对折线和柱状面板生效，且需要打开图例。 */
  legend_placement?: "bottom" | "right"
  /** Grafana 的 Legend width：右侧图例列宽 120..480px；缺省沿用渲染器自带宽度。 */
  legend_width?: number
  tooltip?: TooltipMode
  /** Grafana 的 Panel description：只进入面板头的信息图标 tooltip，不参与查询；上限 512 字节。 */
  description?: string
  unit?: string
  decimals?: number
  min?: number
  max?: number
  thresholds: { value: number; color: PanelColor }[]
  /** 表格单元格显示模式：text/color_text/color_background/gradient；缺省 text，只对 table 面板生效。 */
  cell_display_mode?: DashboardCellDisplayMode
  /** 表格形态：long/wide；缺省 long，只对 table 面板生效。 */
  table_orientation?: DashboardTableOrientation
  /** Grafana 风格的阈值显示模式；缺省表示沿用历史行为（timeseries 虚线 + stat/gauge 取色）。 */
  threshold_mode?: ThresholdDisplayMode
  /** Grafana 的 Value options → Calculation；只对 stat/gauge/bargauge/piechart 生效，缺省等于 last。 */
  calculation?: PanelCalculation
  /** Grafana 的 Value options → Sparkline：只对 stat 面板生效，缺省不画趋势图。 */
  sparkline?: boolean
  /** Grafana 的 Stat styles → Color mode：value（缺省）/background/none；只对 stat 面板生效。 */
  stat_color_mode?: "value" | "background" | "none"
  /** Grafana 的 Stat styles → Text mode：auto（缺省）/value/name/none；只对 stat 面板生效。 */
  stat_text_mode?: "auto" | "value" | "name" | "none"
  /** Grafana 的 Stat styles → Text size：auto（缺省，沿用现有 text-4xl）/sm/md/lg/xl；只对 stat 面板生效。 */
  stat_text_size?: "auto" | "sm" | "md" | "lg" | "xl"
  stat_orientation?: "auto" | "horizontal" | "vertical"
  /** Grafana Gauge → Show threshold markers：缺省显示；只对 gauge 面板生效。 */
  gauge_threshold_markers?: boolean
  /** Grafana Gauge → Show threshold labels：缺省不显示，要求刻度标记未关闭且至少有一条阈值；只对 gauge 面板生效。 */
  gauge_threshold_labels?: boolean
  /** 迷你趋势图的线宽 1..4；缺省 2，只有打开 sparkline 时才有效。 */
  sparkline_line_width?: number
  /** 迷你趋势图下方填充的不透明度百分比 0..100；缺省只画线。 */
  sparkline_fill_opacity?: number
  /** 迷你趋势图填充的渐变：none/opacity/hue；缺省 none，只有打开 sparkline 且填充大于 0 时才有效。 */
  sparkline_gradient_mode?: string
  /** Grafana 风格的值映射：精确值、区间或空值对应显示文本与颜色；只对 stat/gauge/table/bargauge/piechart/statetimeline 生效。 */
  value_mappings?: DashboardValueMapping[]
  /** 条状仪表的排列方向：auto/horizontal/vertical；缺省 auto，只对 bargauge 面板有效。 */
  bar_orientation?: BarOrientation
  /** 条状仪表的填充方式：basic/gradient/lcd；缺省 basic。 */
  bar_display?: BarDisplay
  /** 是否绘制未填充轨道；缺省绘制。 */
  bar_unfilled?: boolean
  /** 饼图形态：pie/donut；缺省 pie，只对 piechart 面板有效。 */
  pie_chart_type?: PieChartType
  /** 饼图扇区标签内容：percent/value/both；缺省 percent，只对 piechart 面板有效。 */
  pie_label_mode?: PieLabelMode
  /** 热力图取值轴分桶数量 2..64；缺省 12，只对 heatmap 面板有效。 */
  heatmap_buckets?: number
  /** 热力图取值轴刻度：linear/log；缺省 linear，只对 heatmap 面板有效。 */
  heatmap_scale?: HeatmapScale
  /** 热力图配色（面板调色板键）；缺省 blue，只对 heatmap 面板有效。 */
  heatmap_color?: PanelColor
  /** 直方图取值分桶数量 2..64；缺省 12，只对 histogram 面板有效。 */
  histogram_buckets?: number
  /** 直方图计数轴刻度：linear/log；缺省 linear，只对 histogram 面板有效。 */
  histogram_scale?: HistogramScale
  /** 直方图合并后的配色（面板调色板键）；缺省 blue，只对 histogram 面板有效。 */
  histogram_color?: PanelColor
  /** 直方图是否把多个序列合并成同一个分布；缺省 false，只对 histogram 面板有效。 */
  histogram_merge?: boolean
  /** 状态时间线是否合并相邻的相同状态；缺省合并，只对 statetimeline 面板有效。 */
  timeline_merge?: boolean
  /** 状态时间线取值标签：auto/always/never；缺省 auto，只对 statetimeline 面板有效。 */
  timeline_show_values?: TimelineShowValues
  /** geo map 地点表：匹配节点 ID、节点名或设备 ID 并给出经纬度；只对 geomap 面板有效。 */
  geo_locations?: DashboardGeoLocation[]
  /** geo map 图层：markers/heatmap；缺省 markers。 */
  geo_layer?: GeoLayer
  /** geo map 的可选 XYZ 底图模板（必须含 {z}/{x}/{y}）；缺省只用内置离线底图。 */
  geo_tile_url?: string
  /** geo map 初始缩放级别 0..8；缺省 0（整个世界）。 */
  geo_zoom?: number
  /** geo map 初始中心；两者必须同时给出，缺省世界中心。 */
  geo_center_latitude?: number
  geo_center_longitude?: number
  /** geo map 标记直径 2..64（世界单位）；缺省 8。 */
  geo_marker_size?: number
  /** 行面板（Grafana Row）的默认折叠状态；缺省展开，只对 row 面板有效。 */
  row_collapsed?: boolean
  series: DashboardSeries[]
  /** Grafana 风格的面板级相对时间覆盖，例如 `now-6h`；留空表示跟随看板范围。 */
  time_from?: string
  /** 覆盖窗口的终点，缺省即 `now`。 */
  time_to?: string
  /** 面板级最大数据点数；缺省继承看板默认，最终仍受查询上限约束。 */
  max_points?: number
  /** Grafana 风格的时长平移，例如 `1d`：窗口整体后移用于对比上一周期。 */
  time_shift?: string
  /** 折线宽度 1..4；缺省为 2。只对 timeseries 面板有效。 */
  line_width?: number
  /** 折线下方填充的不透明度百分比 0..100；缺省只画线。 */
  fill_opacity?: number
  /** 数据点标记：auto/always/never；缺省 auto。 */
  show_points?: "auto" | "always" | "never"
  /** Grafana 的 Point size：数据点半径 1..12；缺省沿用渲染器的 auto 1.8px / always 2.6px。 */
  point_size?: number
  /** Grafana 的 Bar width：单根柱子的最大宽度 4..40px；缺省 12px。只对 barchart 面板有效。 */
  bar_width?: number
  /** Grafana 的 Bar radius：柱顶圆角半径 0..10px；缺省 0（直角）。只对 barchart 面板有效。 */
  bar_radius?: number
  /** Grafana 的 Bar alignment：auto（缺省，等同于 centered）/before/centered/after，只对 barchart 面板有效。 */
  bar_alignment?: BarAlignment
  /** Grafana 的 Soft min：只在数据低于它时撑开坐标轴，否则轴从它开始。只对折线与柱状面板有效。 */
  soft_min?: number
  /** Grafana 的 Soft max：只在数据高于它时撑开坐标轴，否则轴到它结束。只对折线与柱状面板有效。 */
  soft_max?: number
  /** Grafana 的 Axis → Scale：linear（缺省）或 log（以 10 为底）。只对折线与柱状面板有效。 */
  scale?: "linear" | "log"
  /** Grafana 的 Axis → Label：坐标轴旁的短标题（<=40 字节）；留空即不显示。只对折线与柱状面板有效。 */
  axis_label?: string
  /** Grafana 的 Axis → Show grid lines：false 关掉水平网格线；留空等于 true。只对折线与柱状面板有效。 */
  grid?: boolean
  /** Grafana 的 Axis → Placement：left（缺省）或 right。只对折线与柱状面板有效。 */
  axis_placement?: "left" | "right"
  /** Grafana 的 Axis → Centered zero：坐标轴对称展开到 ±|最大值|；与固定 min/max 互斥。 */
  centered_zero?: boolean
  /** 是否堆叠序列：none/normal；缺省 none。 */
  stack?: "none" | "normal" | "percent"
  /** 面积填充的渐变：none/opacity/hue；缺省 none。opacity 渐隐到透明，hue 旋转色相。 */
  gradient_mode?: "none" | "opacity" | "hue"
  /** 折线插值：linear/smooth/step_before/step_after；缺省 linear。只对 timeseries 面板有效。 */
  line_interpolation?: "linear" | "smooth" | "step_before" | "step_after"
  /** Grafana 的 Line style：solid/dashed/dotted；缺省 solid，只对 timeseries 面板有效。 */
  line_pattern?: "solid" | "dashed" | "dotted"
  /** 空样本处理：none 保留缺口，always 跨过缺口连线；缺省 none，只对 timeseries 面板有效。 */
  connect_nulls?: "none" | "always"
  /** Grafana 的 Standard options → No value：字段没有样本时的占位文本；缺省用渲染器的破折号。 */
  no_value?: string
  /** Grafana 的 Transform 标签页：结果先过变换链再渲染；缺省表示原样渲染。 */
  transformations?: DashboardTransformation[]
  /** Grafana 的 Overrides 标签页：按序列标签命中后覆盖颜色、单位与图例可见性。 */
  overrides?: DashboardOverride[]
}

export interface DashboardDefinition {
  /** Grafana 的看板级导航链接；URL 支持 ${...} 占位符。 */
  links?: DashboardPanelLink[]
  /** 只读视图（公开节点页）隐藏看板级链接与面板数据链接；后台工作区始终显示。 */
  hide_links?: boolean
  version: 1
  title: string
  description: string
  time_range_seconds: number
  refresh_seconds: number
  /** 看板默认的最大数据点数；缺省即查询上限。 */
  max_points?: number
  /** 相对时间的 now 回退秒数，避开尚未落库的最新样本。 */
  now_delay_seconds?: number
  /** 看板检索标签；缺省表示未打标签，只用于检索，不影响查询语义。 */
  tags?: string[]
  variables: DashboardVariable[]
  panels: DashboardPanel[]
}

export interface MetricDashboardVersion {
  dashboard_id: string
  revision: number
  title?: string
  summary?: string
  created_at: string
  created_by: string
}

export interface MetricDashboard {
  id: string
  scope: "personal" | "public" | "admin" | "node"
  owner_user_id?: string
  node_id?: string
  revision: number
  definition: DashboardDefinition
  has_previous: boolean
  created_at: string
  updated_at: string
  updated_by?: string
}

export type WorkspaceRange = { mode: "relative"; seconds: number } | { mode: "absolute"; from: string; to: string }

export interface ExpandedSeries extends MetricQuerySeries {
  label: string
  color: PanelColor
  node_name: string
}

export interface PanelQueryState {
  loading: boolean
  query?: MetricQuery
  resultQuery?: MetricQuery
  result?: MetricQueryResult
  expanded?: ExpandedSeries[]
  resultExpanded?: ExpandedSeries[]
  error?: string
  updatedAt?: number
}
