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

// ThemeDefinition 是声明式theme.json；颜色和布局仅能选择受信任渲染器支持的值。
type ThemeDefinition struct {
	SchemaVersion int               `json:"schema_version"`
	Overview      string            `json:"overview"`
	Detail        string            `json:"detail"`
	Density       string            `json:"density"`
	Radius        int               `json:"radius"`
	Light         map[string]string `json:"light"`
	Dark          map[string]string `json:"dark"`
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

// ThemeConfiguration 只允许站点所有者覆盖布局、密度、圆角、字体与内容开关，不接收 CSS 或代码。
// 能写哪些键由主题清单里的 configuration 决定；指针为 nil 表示「未配置」，沿用主题包自己的值。
type ThemeConfiguration struct {
	Overview    *string `json:"overview,omitempty"`
	Detail      *string `json:"detail,omitempty"`
	Density     *string `json:"density,omitempty"`
	Radius      *int    `json:"radius,omitempty"`
	Font        *string `json:"font,omitempty"`
	ShowNetwork *bool   `json:"show_network,omitempty"`
	ShowHost    *bool   `json:"show_host,omitempty"`
	ShowTags    *bool   `json:"show_tags,omitempty"`
	ShowRemark  *bool   `json:"show_remark,omitempty"`
}
