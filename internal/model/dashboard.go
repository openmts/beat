package model

import "time"

const (
	DashboardVersion         = 1
	MaxDashboardBytes        = 64 * 1024
	MaxDashboardPanels       = 24
	MaxDashboardSeries       = 8
	MaxDashboardVariables    = 4
	MaxDashboardTags         = 8
	MaxDashboardThresholds   = 4
	MaxDashboardLegendValues = 4
	// 右侧图例列的像素宽度范围：Grafana 的 Legend → Width，缺省由渲染器给出 160px。
	MinDashboardLegendWidth = 120
	MaxDashboardLegendWidth = 480
	// MaxDashboardTransformations 限制单个面板的变换条数：变换按顺序执行，链路过长会让
	// 「面板为什么这么显示」难以解释，也放大每帧的计算量。
	MaxDashboardTransformations       = 4
	MaxTransformationPatternBytes     = 128
	MaxTransformationReplacementBytes = 120
	// MaxDashboardTransformationCalculations 限制 Reduce → Series to rows 一次展开的计算方式条数：
	// 与 Grafana 一样按「序列 × 计算方式」成长表，条数不设上限会让表格行数成倍膨胀。
	MaxDashboardTransformationCalculations = 4
	// MaxDashboardVariableRegexBytes 限制变量选项过滤正则的长度：它只用于前端过滤已授权的
	// 选项列表，不能成为无界输入。
	MaxDashboardVariableRegexBytes = 64
	MaxPersonalDashboards          = 16
	MaxDashboardRangeSeconds       = 31 * 24 * 60 * 60
	PublicDashboardID              = "public-default"
	AdminDashboardID               = "admin-default"
)

type MetricDashboard struct {
	ID          string              `json:"id"`
	Scope       string              `json:"scope"`
	OwnerUserID string              `json:"owner_user_id,omitempty"`
	NodeID      string              `json:"node_id,omitempty"`
	Revision    int                 `json:"revision"`
	Definition  DashboardDefinition `json:"definition"`
	HasPrevious bool                `json:"has_previous"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	UpdatedBy   string              `json:"updated_by,omitempty"`
}

type DashboardDefinition struct {
	Version     int    `json:"version"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Tags 是看板检索标签；缺省表示未打标签，检索只按标签匹配，不影响查询语义。
	Tags             []string `json:"tags,omitempty"`
	TimeRangeSeconds int      `json:"time_range_seconds"`
	RefreshSeconds   int      `json:"refresh_seconds"`
	// MaxPoints 是看板默认的最大数据点数；nil 表示沿用查询上限。
	MaxPoints *int `json:"max_points,omitempty"`
	// NowDelaySeconds 把相对时间的 now 回退若干秒，避免读到尚未落库的最新样本。
	NowDelaySeconds *int `json:"now_delay_seconds,omitempty"`
	// Links 是看板级导航链接（Grafana 的 Dashboard links）；URL 支持 ${...} 占位符。
	Links []DashboardPanelLink `json:"links,omitempty"`
	// HideLinks 在只读视图（公开节点页、壁板）里隐藏看板链接条与面板数据链接：这些入口往往指向
	// 后台运维页，作者可以不把它们暴露给终端用户；后台工作区始终显示，便于继续编辑。
	HideLinks *bool               `json:"hide_links,omitempty"`
	Variables []DashboardVariable `json:"variables"`
	Panels    []DashboardPanel    `json:"panels"`
}

type DashboardVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
	// Multi 允许同时选择多个选项；默认单选，保持既有看板的行为不变。
	Multi bool `json:"multi,omitempty"`
	// IncludeAll 在多选变量中提供 Grafana 的 All 选项；只能与 Multi 同用。
	IncludeAll bool `json:"include_all,omitempty"`
	// Regex 按 RE2 语义过滤可选项；空值表示不过滤。
	Regex string `json:"regex,omitempty"`
}

type DashboardPanel struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Size   string `json:"size"`
	Legend bool   `json:"legend"`
	// LegendPlacement 是图例位置：bottom（缺省）/right；只对会画图例的折线与柱状面板有效，
	// 且要求 Legend 打开，否则就是「配置写了但永远不渲染」的看板。
	LegendPlacement string `json:"legend_placement,omitempty"`
	// LegendWidth 是右侧图例列的像素宽度（见 MinDashboardLegendWidth/MaxDashboardLegendWidth）；
	// nil 表示渲染器缺省的 160px。只在图例打开且位置为 right 时有效。
	LegendWidth  *int     `json:"legend_width,omitempty"`
	LegendValues []string `json:"legend_values,omitempty"`
	LegendSort   string   `json:"legend_sort,omitempty"`
	Tooltip      string   `json:"tooltip,omitempty"`
	// Description 是 Grafana 的 Panel description：只用于面板头的信息图标 tooltip，
	// 不参与查询与渲染；空值表示不显示图标，长度上限见 MaxPanelDescriptionBytes。
	Description string `json:"description,omitempty"`
	// Repeat 指定按变量逐节点重复渲染的变量名；空值表示单面板。
	Repeat string `json:"repeat,omitempty"`
	// TimeFrom/TimeTo 是 Grafana 风格的面板级相对时间覆盖，例如 `now-6h`；留空表示跟随看板范围。
	TimeFrom string `json:"time_from,omitempty"`
	TimeTo   string `json:"time_to,omitempty"`
	// MaxPoints 覆盖看板默认的最大数据点数；nil 表示继承看板或查询上限。
	MaxPoints *int `json:"max_points,omitempty"`
	// TimeShift 把面板窗口整体后移，例如 `1d` 用于对比上一周期；空值表示不平移。
	TimeShift string `json:"time_shift,omitempty"`
	// LineWidth 是折线宽度（1..4，见 MinDashboardLineWidth/MaxDashboardLineWidth）；nil 表示默认的 2。
	LineWidth *int `json:"line_width,omitempty"`
	// FillOpacity 是折线下方填充的不透明度百分比 0..100；nil 表示只画线。
	FillOpacity *int `json:"fill_opacity,omitempty"`
	// ShowPoints 控制数据点标记：auto/always/never；空值等于 auto。
	ShowPoints string `json:"show_points,omitempty"`
	// PointSize 是数据点标记半径 1..12（Grafana 的 Point size）；nil 表示渲染器的既有口径
	// （always 2.6px、auto 1.8px）。只对折线面板有效，且要求数据点可见。
	PointSize *int `json:"point_size,omitempty"`
	// Stack 控制是否堆叠序列：none/normal；空值等于 none。
	Stack string `json:"stack,omitempty"`
	// GradientMode 控制面积填充的渐变：none/opacity/hue；空值等于 none。opacity 让填充从
	// 折线色渐隐到透明，hue 在保持亮度与透明度的同时旋转色相，便于堆叠序列分层。
	GradientMode string `json:"gradient_mode,omitempty"`
	// LineInterpolation 是折线插值方式：linear/smooth/step_before/step_after；空值等于 linear。
	// 取值只选择渲染器内置的曲线算法，不接收自定义路径，柱状图与取值面板同样不接受它。
	LineInterpolation string `json:"line_interpolation,omitempty"`
	// LinePattern 是折线线型：solid（缺省）/dashed/dotted；只对 timeseries 面板有效。
	// 与线宽、数据点、插值同一口径：柱状图与其它面板类型带上它一律拒绝。
	LinePattern string `json:"line_pattern,omitempty"`
	// ConnectNulls 控制空样本处是否把折线连起来：none（缺省）/always；只对 timeseries 面板有效。
	// none 保留采样缺口（Grafana 的 never），always 跨过缺口连线（Grafana 的 always）。
	ConnectNulls string `json:"connect_nulls,omitempty"`
	// StatColorMode 是值面板的着色方式：value（缺省，给数值文字上阈值色）/background（给面板底色）/
	// none（不着色）；只对 stat 面板有效。
	StatColorMode string `json:"stat_color_mode,omitempty"`
	// StatTextMode 是值面板显示的内容：auto（缺省，数值 + 系列名 + 采样时间）/value（只显示数值）/
	// name（主文本显示系列名）/none（不显示文本）；只对 stat 面板有效。
	StatTextMode string `json:"stat_text_mode,omitempty"`
	// StatTextSize 是值面板主文本的字号档位：auto（缺省）/sm/md/lg/xl；只对 stat 面板有效。
	StatTextSize string `json:"stat_text_size,omitempty"`
	// StatOrientation 是值面板的排版方向：auto（缺省，数值在上、名称在下）/horizontal（名称与数值同行）/
	// vertical（名称在上、数值在下）；只对 stat 面板有效。
	StatOrientation string `json:"stat_orientation,omitempty"`
	// GaugeThresholdMarkers 是 Grafana Gauge → Show threshold markers：在仪表条上按阈值位置画刻度，
	// nil 表示显示（与 Grafana 缺省一致）；只对 gauge 面板有效。
	GaugeThresholdMarkers *bool `json:"gauge_threshold_markers,omitempty"`
	// GaugeThresholdLabels 是 Grafana Gauge → Show threshold labels：在刻度旁标出阈值数值，
	// nil 表示不显示；要求刻度标记未关闭且至少有一条阈值，只对 gauge 面板有效。
	GaugeThresholdLabels *bool `json:"gauge_threshold_labels,omitempty"`
	// BarOrientation 是条状仪表的排列方向：auto/horizontal/vertical；空值等于 auto。
	BarOrientation string `json:"bar_orientation,omitempty"`
	// BarDisplay 是条状仪表的填充方式：basic/gradient/lcd；空值等于 basic。
	BarDisplay string `json:"bar_display,omitempty"`
	// BarUnfilled 控制是否绘制未填充轨道；nil 表示绘制。
	BarUnfilled *bool `json:"bar_unfilled,omitempty"`
	// BarWidth 是单根柱子的最大宽度 4..40px（Grafana 的 Bar width）；nil 表示渲染器缺省的 12px。
	BarWidth *int `json:"bar_width,omitempty"`
	// BarRadius 是柱顶圆角半径 0..10px（Grafana 的 Bar radius）；nil 等于 0（直角）。
	BarRadius *int `json:"bar_radius,omitempty"`
	// BarAlignment 是 Grafana 的 Bar alignment：auto（缺省，等同于 centered）/before（柱子结束于时间点）/
	// centered（柱子中心在时间点）/after（柱子起始于时间点）；只对 barchart 面板有效。
	BarAlignment string `json:"bar_alignment,omitempty"`
	// PieChartType 是饼图形态：pie/donut；空值等于 pie。
	PieChartType string `json:"pie_chart_type,omitempty"`
	// PieLabelMode 是饼图扇区标签内容：percent/value/both；空值等于 percent。
	PieLabelMode string `json:"pie_label_mode,omitempty"`
	// HeatmapBuckets 是热力图取值轴的分桶数量（2..64，见 MinDashboardHeatmapBuckets/
	// MaxDashboardHeatmapBuckets）；nil 表示默认的 12。
	HeatmapBuckets *int `json:"heatmap_buckets,omitempty"`
	// HeatmapScale 是热力图取值轴刻度：linear/log；空值等于 linear。
	HeatmapScale string `json:"heatmap_scale,omitempty"`
	// HeatmapColor 是热力图配色（面板调色板键）；空值等于 blue。
	HeatmapColor string `json:"heatmap_color,omitempty"`
	// HistogramBuckets 是直方图取值轴的分桶数量（2..64，见 MinDashboardHistogramBuckets/
	// MaxDashboardHistogramBuckets）；nil 表示默认的 12。
	HistogramBuckets *int `json:"histogram_buckets,omitempty"`
	// HistogramScale 是直方图计数轴刻度：linear/log；空值等于 linear。
	HistogramScale string `json:"histogram_scale,omitempty"`
	// HistogramColor 是直方图合并后的配色（面板调色板键）；空值等于 blue。
	HistogramColor string `json:"histogram_color,omitempty"`
	// HistogramMerge 决定直方图是否把多个序列合并成同一个分布；nil 表示不合并。
	HistogramMerge *bool `json:"histogram_merge,omitempty"`
	// Text 是文本面板的 Markdown 内容（见 MaxDashboardTextBytes）；只对 text 面板生效。
	Text string `json:"text,omitempty"`
	// Links 是 Grafana 风格的面板数据链接（钻取入口）；URL 支持 ${...} 占位符，
	// 空值表示该面板没有链接。
	Links []DashboardPanelLink `json:"links,omitempty"`
	// RowCollapsed 是行面板的默认折叠状态：nil 表示展开；只对 row 面板生效，
	// 其它面板类型带上它会被拒绝。
	RowCollapsed *bool `json:"row_collapsed,omitempty"`
	// TimelineMerge 决定状态时间线是否把相邻的相同状态合并成一个色块；nil 表示合并。
	TimelineMerge *bool `json:"timeline_merge,omitempty"`
	// GeoLocations 是 geo map 的地点表：把序列匹配到的节点/设备映射到经纬度；
	// 只对 geomap 面板生效，其它面板类型带上它会被拒绝。
	GeoLocations []DashboardGeoLocation `json:"geo_locations,omitempty"`
	// GeoLayer 是 geo map 的图层：markers/heatmap；空值等于 markers。
	GeoLayer string `json:"geo_layer,omitempty"`
	// GeoTileURL 是可选的 XYZ 底图模板（必须含 {z}/{x}/{y} 占位符）；空值表示只用内置离线路网底图。
	GeoTileURL string `json:"geo_tile_url,omitempty"`
	// GeoZoom 是初始缩放级别（0..8，见 MinDashboardGeoZoom/MaxDashboardGeoZoom）；nil 表示 0（整个世界）。
	GeoZoom *int `json:"geo_zoom,omitempty"`
	// GeoCenterLatitude / GeoCenterLongitude 是初始中心；两者必须同时给出，nil 表示世界中心。
	GeoCenterLatitude  *float64 `json:"geo_center_latitude,omitempty"`
	GeoCenterLongitude *float64 `json:"geo_center_longitude,omitempty"`
	// GeoMarkerSize 是标记直径（2..64 世界单位）；nil 表示 8。
	GeoMarkerSize *int `json:"geo_marker_size,omitempty"`
	// TimelineShowValues 控制状态时间线的取值标签：auto/always/never；空值等于 auto。
	TimelineShowValues string   `json:"timeline_show_values,omitempty"`
	Unit               string   `json:"unit,omitempty"`
	Decimals           *int     `json:"decimals,omitempty"`
	Min                *float64 `json:"min,omitempty"`
	Max                *float64 `json:"max,omitempty"`
	// SoftMin/SoftMax 是 Grafana 的 Soft min / Soft max：只在数据超出时把坐标轴撑开，
	// 数据没超出时轴就从这里开始。只对折线与柱状面板生效，且同一侧已经有固定 min/max 时拒绝。
	SoftMin *float64 `json:"soft_min,omitempty"`
	SoftMax *float64 `json:"soft_max,omitempty"`
	// Scale 是 Grafana 的 Axis → Scale：留空与 linear 等效，"log" 表示以 10 为底的对数刻度。
	// 与柔性上下限同规则：只对折线与柱状面板生效，对数轴的下限必须大于 0。
	Scale string `json:"scale,omitempty"`
	// AxisLabel 是 Grafana 的 Axis → Label：坐标轴旁的一行短标题，留空即不显示。
	// 与刻度类型同规则，只对折线与柱状面板生效。
	AxisLabel string `json:"axis_label,omitempty"`
	// Grid 是 Grafana 的 Axis → Show grid lines：false 关掉水平网格线，留空等于 true。
	Grid *bool `json:"grid,omitempty"`
	// AxisPlacement 是 Grafana 的 Axis → Placement：left 或 right，留空等于 auto（左侧）。
	AxisPlacement string `json:"axis_placement,omitempty"`
	// CenteredZero 是 Grafana 的 Axis → Centered zero：坐标轴对称展开到 ±|最大值|。
	// 与固定 min/max 冲突（固定值直接决定坐标轴），只对折线与柱状面板生效。
	CenteredZero bool                 `json:"centered_zero,omitempty"`
	Thresholds   []DashboardThreshold `json:"thresholds"`
	// ThresholdMode 是阈值显示模式（off/line/area/background）；留空表示沿用既有行为：
	// timeseries 画阈值虚线，stat/gauge 按阈值取色。
	ThresholdMode string `json:"threshold_mode,omitempty"`
	// Calculation 是值计算方式（last/min/max/mean/sum/count）；留空等于 last（最新有效样本）。
	// 只对 stat/gauge/bargauge/piechart 生效：折线与表格保留原始采样。
	Calculation string `json:"calculation,omitempty"`
	// Sparkline 打开 stat 面板的迷你趋势图（Grafana 的 Value options → Sparkline）；nil 表示关闭。
	// 只对 stat 面板生效，其它面板类型带上它会被拒绝。
	Sparkline *bool `json:"sparkline,omitempty"`
	// SparklineLineWidth 是迷你趋势图的线宽（1..4，见 MinDashboardSparklineLineWidth/
	// MaxDashboardSparklineLineWidth）；nil 表示默认的 2，只在与 Sparkline 同用时才有效。
	SparklineLineWidth *int `json:"sparkline_line_width,omitempty"`
	// SparklineFillOpacity 是迷你趋势图下方的填充不透明度百分比（0..100，见
	// MaxDashboardSparklineFillOpacity）；nil 表示只画线，同样要求先打开 Sparkline。
	SparklineFillOpacity *int `json:"sparkline_fill_opacity,omitempty"`
	// SparklineGradientMode 是迷你趋势图填充的渐变：none/opacity/hue；空值等于 none。
	// 与折线的 GradientMode 同义，但只作用于 stat 面板的迷你趋势图，因此单独成字段；
	// 取值非 none 时要求填充不透明度大于 0，否则渐变写了也看不出来。
	SparklineGradientMode string `json:"sparkline_gradient_mode,omitempty"`
	// CellDisplayMode 是表格单元格的显示模式（Grafana 的 Cell display mode）：text/color_text/
	// color_background/gradient；nil 表示沿用 text。只对 table 面板生效，其它类型带上它会被拒绝。
	CellDisplayMode *string `json:"cell_display_mode,omitempty"`
	// TableOrientation 是表格面板的形态（Grafana 的 Table 布局）：long 是历史默认——一行一个
	// 采样，列固定为时间/序列/数值；wide 是 Grafana 默认的宽表——一列一条序列、一行一个时间点。
	// nil 表示 long。只对 table 面板生效，其它类型带上它会被拒绝。
	TableOrientation *string `json:"table_orientation,omitempty"`
	// ValueMappings 把数值映射成显示文本与颜色（Grafana 的 Value mappings）；只对 stat/gauge/table/bargauge/piechart 生效。
	ValueMappings []DashboardValueMapping `json:"value_mappings,omitempty"`
	// Transformations 是 Grafana 风格的 Transform 标签页：查询结果先按顺序过一遍变换链，再交给渲染。
	Transformations []DashboardTransformation `json:"transformations,omitempty"`
	// NoValue 是 Grafana 的 Standard options → No value：某个字段没有样本时显示的占位文本；
	// 空值表示沿用渲染器的默认破折号。只有会显示占位文本的面板消费它。
	NoValue string `json:"no_value,omitempty"`
	// Overrides 是 Grafana 风格的 Overrides 标签页：按序列标签命中后覆盖该序列的显示属性。
	// 覆盖只改显示，不改采样值与查询；命中的覆盖按数组顺序叠加，后面的覆盖赢。
	Overrides []DashboardOverride `json:"overrides,omitempty"`
	Series    []DashboardSeries   `json:"series"`
}

// DashboardOverride 是一条字段覆盖规则：Matcher 决定命中哪些序列，其余字段是覆盖后的显示属性。
// 与其它面板字段一样只有显式写下的属性才生效，nil 表示沿用面板或序列的默认值。
type DashboardOverride struct {
	Matcher DashboardOverrideMatcher `json:"matcher"`
	// Color 是命中序列的调色板键；空值表示不改颜色。
	Color string `json:"color,omitempty"`
	// Unit 是命中序列的显示单位键（≤32 字节）；空值表示不改单位。
	Unit string `json:"unit,omitempty"`
	// HideLegend 把命中序列从图例里隐藏；面板仍然绘制该序列。
	HideLegend *bool `json:"hide_legend,omitempty"`
	// LineWidth 覆盖命中序列的折线宽度（1..4）；nil 表示沿用面板声明的线宽。
	LineWidth *int `json:"line_width,omitempty"`
	// FillOpacity 覆盖命中序列的填充不透明度（0..100，百分比）；nil 表示沿用面板声明的填充。
	FillOpacity *int `json:"fill_opacity,omitempty"`
	// AxisPlacement 把命中序列挪到左轴或右轴（Grafana 的 Overrides → Axis → Placement）；
	// 空值表示沿用面板声明的轴位置。只有折线与柱状面板消费它。
	AxisPlacement string `json:"axis_placement,omitempty"`
	// Thresholds 给命中序列换一套阈值色带（Grafana 的 Overrides → Thresholds）；空值表示沿用面板
	// 声明的阈值。只有按阈值给单值取色的面板消费它。
	Thresholds []DashboardThreshold `json:"thresholds,omitempty"`
	// FillBelowTo 是另一条序列的名称：命中序列的填充区延伸到它（Grafana 的 Overrides →
	// Graph styles → Fill below to）。只有不堆叠的折线面板消费它，空值表示填充到坐标轴。
	FillBelowTo string `json:"fill_below_to,omitempty"`
	// AxisLabel 给命中序列所在的值轴换一行标题（Grafana 的 Overrides → Axis → Label）；空值表示
	// 沿用面板级 axis_label。多条覆盖落在同一根轴上时按覆盖顺序叠加，后面的赢。
	AxisLabel string `json:"axis_label,omitempty"`
	// AxisSoftMin/AxisSoftMax 给命中序列所在的值轴设置柔性上下限（Grafana 的 Overrides → Axis →
	// Soft min / Soft max）：数据没超出时轴从这里开始，超出时自动撑开。面板本身声明了固定上下限时
	// 柔性值永远不会生效，因此那种组合会被拒绝。
	AxisSoftMin *float64 `json:"axis_soft_min,omitempty"`
	AxisSoftMax *float64 `json:"axis_soft_max,omitempty"`
	// AxisWidth 覆盖命中序列所在值轴的宽度（Grafana 的 Overrides → Axis → Width）；nil 表示按单位与
	// 轴标题自动计算。与 Grafana 一样 0 也等于自动，只有折线与柱状面板消费它。
	AxisWidth *int `json:"axis_width,omitempty"`
	// DisplayName 覆盖命中序列的显示名（Grafana 的 Overrides → Standard options → Display name）；
	// 空值表示沿用序列标签。图例、提示框、表格列头与取值面板都用它，行与文本面板不能带覆盖。
	DisplayName string `json:"display_name,omitempty"`
	// Decimals 覆盖命中序列的小数位数（Grafana 的 Overrides → Standard options → Decimals）；nil 表示
	// 沿用面板级 decimals，取值 0..6。
	Decimals *int `json:"decimals,omitempty"`
	// Min/Max 覆盖命中序列的值域（Grafana 的 Overrides → Standard options → Min / Max）：只有把单值
	// 映射到一段量程的 stat / gauge / bargauge 消费它们，其余面板没有量程。
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// DashboardOverrideMatcher 决定覆盖命中哪些序列：by_name 精确匹配序列标签，by_regex 按 RE2 正则匹配标签，
// by_ref_id 精确匹配查询序列 id（Grafana 的 Overrides → Fields by query），by_type 匹配序列的单位族
// （Grafana 的 Fields with type；Beat 的序列值都是数值，单位是它唯一的稳定类型维度），
// by_value 匹配最新样本等于给定数值的序列（Grafana 的 Fields with values）。
type DashboardOverrideMatcher struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// DashboardTransformation 是一条面板级结果变换。字段按种类二选一使用：不属于该种类的字段
// 必须留空，校验器会拒绝混用的负载，避免「配置写了但静默不生效」。
type DashboardTransformation struct {
	Kind string `json:"kind"`
	// RenameByRegex 使用 Pattern/Replacement：RE2 正则全局替换序列标签。
	Pattern     string `json:"pattern,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	// FilterByValue 使用 Comparison/Value：按序列最新有效样本与阈值比较，保留命中的序列。
	Comparison string   `json:"comparison,omitempty"`
	Value      *float64 `json:"value,omitempty"`
	// Column 把 FilterByValue 从「按序列取值过滤」改成「按结果帧的这一列过滤行」（Grafana 的
	// Filter data by values 允许选字段）：给出列名时这一步只作用在帧的行上，序列形态不受影响。
	Column string `json:"column,omitempty"`
	// SortBy 使用 Statistic/Order：按图例统计量排序；Limit 使用 Count 保留前 N 条。
	Statistic string `json:"statistic,omitempty"`
	Order     string `json:"order,omitempty"`
	Count     *int   `json:"count,omitempty"`
	// Field 让 SortBy 直接指定结果帧的列（含分组键列）；不给出时沿用 Statistic 的同名计算列匹配。
	Field string `json:"field,omitempty"`
	// FilterByLabel 复用 Pattern：按序列标签的 RE2 正则过滤，Invert 反转命中集合。
	Invert bool `json:"invert,omitempty"`
	// BinaryOperation 使用 Left/Operator/Right/Alias：按时间戳对齐两条序列再做四则运算，
	// 结果作为一条新序列追加在末尾（Grafana 的 Add field from calculation → Binary operation）。
	Left     string `json:"left,omitempty"`
	Operator string `json:"operator,omitempty"`
	Right    string `json:"right,omitempty"`
	Alias    string `json:"alias,omitempty"`
	// Reduce 使用 Calculation / Calculations 与 Mode：把每条序列按窗口压成结果（Grafana 的
	// Transform → Reduce），取值集合与面板级 Calculation 共用一份，避免同一个概念出现两套语义。
	// 历史看板写单个 Calculation；新看板可用 Calculations 多选，此时 Mode 必须是 series_to_rows。
	Calculation  string   `json:"calculation,omitempty"`
	Calculations []string `json:"calculations,omitempty"`
	// Mode 留空等于 reduce_fields：每条序列压成一个样本。series_to_rows 把「每条序列 × 每个
	// 计算方式」展开成长表行，表格面板因此能逐行读取每个计算方式的结果。
	Mode string `json:"mode,omitempty"`
	// GroupBy 使用 GroupBy/Calculation：按序列的某个身份字段分组，再把组内多条序列按时间点聚合成
	// 一条（Grafana 的 Transform → Group by）。
	GroupBy string `json:"group_by,omitempty"`
}

type DashboardSeries struct {
	DeviceID    string `json:"device_id,omitempty"`
	RefID       string `json:"ref_id"`
	NodeID      string `json:"node_id"`
	Metric      string `json:"metric"`
	Aggregation string `json:"aggregation"`
	Label       string `json:"label"`
	Color       string `json:"color"`
}

type DashboardThreshold struct {
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

// DashboardValueMapping 是 Grafana 风格的值映射：精确值、区间或特殊值（null/NaN）对应一段文本与颜色。
// 未命中任何映射时仍按数值与阈值渲染，所以映射是可选覆盖而不是必填规则。
type DashboardValueMapping struct {
	Type  string   `json:"type"`
	Value *float64 `json:"value,omitempty"`
	From  *float64 `json:"from,omitempty"`
	To    *float64 `json:"to,omitempty"`
	Match string   `json:"match,omitempty"`
	Text  string   `json:"text,omitempty"`
	Color string   `json:"color,omitempty"`
}

// DashboardMetricPolicy 仅包含验证布局所需的目录策略，不包含采集命令。
type DashboardMetricPolicy struct {
	Unit         string
	Aggregations []string
	Public       bool
	Enabled      bool
}

type DashboardPolicy struct {
	Metrics map[string]DashboardMetricPolicy
	Nodes   map[string]bool
	Public  bool
}
