package model

import "slices"

// 主题自声明配置的种类（komari 的 configuration.type）。只支持 managed：raw 让主题自带一份配置
// HTML，redirect 把配置页指到外部地址，两者都要把站点所有者的会话交给主题或第三方页面，
// 风险与收益不成比例，明确不做。
const (
	ThemeConfigurationManaged = "managed"
)

// 自声明配置项的类型由键决定，作者不能自定义：select 是固定取值，number 是整数区间，switch 是开/关。
const (
	ThemeConfigurationSelect = "select"
	ThemeConfigurationNumber = "number"
	ThemeConfigurationSwitch = "switch"
)

// ThemeConfigurationItemLimit 是一份清单里允许声明的配置项数量上限。
const ThemeConfigurationItemLimit = 12

// 圆角半径的合法区间，渲染口径与配置校验共用。
const (
	ThemeRadiusMinimum = 0
	ThemeRadiusMaximum = 8
)

// ThemeConfigurationKeys 是可被主题声明为「站点可配置」的显示字段。取值都落在既有的渲染口径内，
// 因此配置永远改不了配色、结构或任何代码：resources 这类数组字段不在其中。
var ThemeConfigurationKeys = []string{"overview", "detail", "density", "radius", "font", "show_network", "show_host", "show_tags", "show_remark"}

// ThemeConfigurationKeyValues 给出 select 型字段的合法取值；其它类型的键返回 nil。
func ThemeConfigurationKeyValues(key string) []string {
	switch key {
	case "overview":
		return []string{"cards", "table", "tiles", "list"}
	case "detail":
		return []string{"balanced", "compact", "spotlight"}
	case "density":
		return []string{"comfortable", "compact"}
	case "font":
		return ThemeFonts
	default:
		return nil
	}
}

// ThemeConfigurationKeyOptions 给出某个键最终生效的取值集合：作者用 options 收窄过就用收窄后的，
// 否则用该键的全集；非 select 键返回 nil。
func ThemeConfigurationKeyOptions(key string, narrowed []string) []string {
	if len(narrowed) > 0 {
		return narrowed
	}
	return ThemeConfigurationKeyValues(key)
}

// ThemeConfigurationKeyType 给出键的类型；未知键返回空串。
func ThemeConfigurationKeyType(key string) string {
	if values := ThemeConfigurationKeyValues(key); len(values) > 0 {
		return ThemeConfigurationSelect
	}
	switch key {
	case "radius":
		return ThemeConfigurationNumber
	case "show_network", "show_host", "show_tags", "show_remark":
		return ThemeConfigurationSwitch
	default:
		return ""
	}
}

// IsThemeConfigurationKey 报告该键是否可以被主题声明为站点可配置。
func IsThemeConfigurationKey(key string) bool {
	return slices.Contains(ThemeConfigurationKeys, key)
}

// ThemeConfigurationSchema 是主题自声明的站点配置架构；nil 表示沿用历史行为（只允许四个显示字段）。
// Type 与每个 item 的 Type 由服务端按键推导后下发，作者写进清单的值会被拒绝。
type ThemeConfigurationSchema struct {
	Type  string                   `json:"type"`
	Items []ThemeConfigurationItem `json:"items"`
}

// ThemeConfigurationItem 是作者声明的一项可配置字段：Name 用于后台表单，Options 可以进一步收窄取值。
type ThemeConfigurationItem struct {
	Key     string   `json:"key"`
	Name    string   `json:"name,omitempty"`
	Type    string   `json:"type,omitempty"`
	Options []string `json:"options,omitempty"`
}
