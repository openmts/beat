package model

// 主题可渲染的资源行；顺序即声明顺序，最多三项。
const (
	ThemeResourceCPU    = "cpu"
	ThemeResourceMemory = "memory"
	ThemeResourceDisk   = "disk"
)

// ThemeResources 是作者可以选择的资源行全集，顺序与默认渲染顺序一致。
var ThemeResources = []string{ThemeResourceCPU, ThemeResourceMemory, ThemeResourceDisk}

// 主题可选的字体族；省略表示沿用产品默认字体，只允许受信任渲染器内置的固定字体栈。
const (
	ThemeFontSystem = "system"
	ThemeFontSerif  = "serif"
	ThemeFontMono   = "mono"
)

// ThemeFonts 是作者可以选择的字体族全集，顺序即文档顺序。
var ThemeFonts = []string{ThemeFontSystem, ThemeFontSerif, ThemeFontMono}

// 主题表面样式；只映射渲染器内置的边框、阴影与透明层，不接受任意 CSS。
const (
	ThemeSurfaceSolid   = "solid"
	ThemeSurfaceSoft    = "soft"
	ThemeSurfaceOutline = "outline"
	ThemeSurfaceFlat    = "flat"
)

var ThemeSurfaces = []string{ThemeSurfaceSolid, ThemeSurfaceSoft, ThemeSurfaceOutline, ThemeSurfaceFlat}

// 首页顶部概览的形态；metrics 决定显示项与顺序。
const (
	ThemeSummaryCards   = "cards"
	ThemeSummaryStrip   = "strip"
	ThemeSummaryFocus   = "focus"
	ThemeSummaryMinimal = "minimal"
)

var ThemeSummaryLayouts = []string{ThemeSummaryCards, ThemeSummaryStrip, ThemeSummaryFocus, ThemeSummaryMinimal}

// 首页概览可用的统计项；全部由 Beat 从公开节点数据计算。
const (
	ThemeSummaryAvailability = "availability"
	ThemeSummaryBusiest      = "busiest"
	ThemeSummaryTraffic      = "traffic"
	ThemeSummaryThroughput   = "throughput"
	ThemeSummaryGroups       = "groups"
)

var ThemeSummaryMetrics = []string{
	ThemeSummaryAvailability,
	ThemeSummaryBusiest,
	ThemeSummaryTraffic,
	ThemeSummaryThroughput,
	ThemeSummaryGroups,
}

// 页面外壳背景与页头样式；装饰只使用主题调色板和渲染器内置图案。
const (
	ThemeShellPlain     = "plain"
	ThemeShellWash      = "wash"
	ThemeShellGrid      = "grid"
	ThemeShellSpotlight = "spotlight"

	ThemeHeaderGlass       = "glass"
	ThemeHeaderSolid       = "solid"
	ThemeHeaderTransparent = "transparent"

	ThemeFooterPlain   = "plain"
	ThemeFooterInset   = "inset"
	ThemeFooterMinimal = "minimal"
)

var ThemeShellBackgrounds = []string{ThemeShellPlain, ThemeShellWash, ThemeShellGrid, ThemeShellSpotlight}

var ThemeShellHeaders = []string{ThemeHeaderGlass, ThemeHeaderSolid, ThemeHeaderTransparent}

var ThemeShellFooters = []string{ThemeFooterPlain, ThemeFooterInset, ThemeFooterMinimal}

// ThemeDefinition 是声明式theme.json；颜色和布局仅能选择受信任渲染器支持的值。
type ThemeDefinition struct {
	SchemaVersion int               `json:"schema_version"`
	Overview      string            `json:"overview"`
	Detail        string            `json:"detail"`
	Density       string            `json:"density"`
	Radius        int               `json:"radius"`
	Light         map[string]string `json:"light"`
	Dark          map[string]string `json:"dark"`
	// Surface 控制概览卡、节点卡与详情面板的表面层级。
	Surface string `json:"surface,omitempty"`
	// Summary 控制首页顶部概览的布局、统计项与顺序。
	Summary *ThemeSummary `json:"summary,omitempty"`
	// Shell 控制公开页面的背景纹理、页头与页脚样式。
	Shell *ThemeShell `json:"shell,omitempty"`
	// Resources 选择并排序列表里展示的资源行；空值表示全部资源，按默认顺序渲染。
	Resources []string `json:"resources,omitempty"`
	// 以下开关的 nil 表示沿用默认值 true；作者显式写 false 才隐藏对应内容。
	ShowNetwork *bool `json:"show_network,omitempty"`
	ShowHost    *bool `json:"show_host,omitempty"`
	ShowTags    *bool `json:"show_tags,omitempty"`
	ShowRemark  *bool `json:"show_remark,omitempty"`
	// Font 选择公开页的字体栈；空值表示沿用产品默认字体，不接受自定义字体或外部字体地址。
	Font string `json:"font,omitempty"`
}

// ThemeSummary 是首页顶部概览的受控声明。
type ThemeSummary struct {
	Layout  string   `json:"layout,omitempty"`
	Metrics []string `json:"metrics,omitempty"`
}

// ThemeShell 是公开页面外壳的受控声明。
type ThemeShell struct {
	Background string `json:"background,omitempty"`
	Header     string `json:"header,omitempty"`
	Footer     string `json:"footer,omitempty"`
}

// ThemeConfiguration 只允许站点所有者覆盖布局、密度、圆角、字体与内容开关，不接收 CSS 或代码。
// 能写哪些键由主题清单里的 configuration 决定；指针为 nil 表示「未配置」，沿用主题包自己的值。
type ThemeConfiguration struct {
	Overview        *string `json:"overview,omitempty"`
	Detail          *string `json:"detail,omitempty"`
	Density         *string `json:"density,omitempty"`
	Radius          *int    `json:"radius,omitempty"`
	Font            *string `json:"font,omitempty"`
	Surface         *string `json:"surface,omitempty"`
	SummaryLayout   *string `json:"summary_layout,omitempty"`
	ShellBackground *string `json:"shell_background,omitempty"`
	ShellHeader     *string `json:"shell_header,omitempty"`
	ShellFooter     *string `json:"shell_footer,omitempty"`
	ShowNetwork     *bool   `json:"show_network,omitempty"`
	ShowHost        *bool   `json:"show_host,omitempty"`
	ShowTags        *bool   `json:"show_tags,omitempty"`
	ShowRemark      *bool   `json:"show_remark,omitempty"`
}
