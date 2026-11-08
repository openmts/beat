export const THEME_PALETTE_TOKENS = [
  "background", "foreground", "card", "card-foreground", "popover", "popover-foreground", "primary", "primary-foreground",
  "secondary", "secondary-foreground", "muted", "muted-foreground", "accent", "accent-foreground", "destructive", "destructive-foreground",
  "border", "input", "ring", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
] as const
export const THEME_RESOURCE_TOKENS = ["cpu", "memory", "disk"] as const
export type ThemeResource = (typeof THEME_RESOURCE_TOKENS)[number]
export const THEME_FONT_TOKENS = ["system", "serif", "mono"] as const
export type ThemeFont = (typeof THEME_FONT_TOKENS)[number]
export const THEME_SURFACE_TOKENS = ["solid", "soft", "outline", "flat"] as const
export type ThemeSurface = (typeof THEME_SURFACE_TOKENS)[number]
export const THEME_SUMMARY_LAYOUT_TOKENS = ["cards", "strip", "focus", "minimal"] as const
export type ThemeSummaryLayout = (typeof THEME_SUMMARY_LAYOUT_TOKENS)[number]
export const THEME_SUMMARY_METRIC_TOKENS = ["availability", "busiest", "traffic", "throughput", "groups"] as const
export type ThemeSummaryMetric = (typeof THEME_SUMMARY_METRIC_TOKENS)[number]
export const THEME_SHELL_BACKGROUND_TOKENS = ["plain", "wash", "grid", "spotlight"] as const
export type ThemeShellBackground = (typeof THEME_SHELL_BACKGROUND_TOKENS)[number]
export const THEME_SHELL_HEADER_TOKENS = ["glass", "solid", "transparent"] as const
export type ThemeShellHeader = (typeof THEME_SHELL_HEADER_TOKENS)[number]
export const THEME_SHELL_FOOTER_TOKENS = ["plain", "inset", "minimal"] as const
export type ThemeShellFooter = (typeof THEME_SHELL_FOOTER_TOKENS)[number]

export interface ThemeSummaryDefinition {
  /** 省略时使用 cards；metrics 省略时展示全部默认四项。 */
  layout?: ThemeSummaryLayout
  /** 首页顶部统计项及其顺序；最多五项，不允许重复。 */
  metrics?: ThemeSummaryMetric[]
}

export interface ThemeShellDefinition {
  /** 页面背景纹理，只使用主题调色板生成。 */
  background?: ThemeShellBackground
  header?: ThemeShellHeader
  footer?: ThemeShellFooter
}

export interface ThemeDefinition {
  schema_version: 1
  overview: "cards" | "table" | "tiles" | "list"
  detail: "balanced" | "compact" | "spotlight"
  density: "comfortable" | "compact"
  radius: number
  light: Record<(typeof THEME_PALETTE_TOKENS)[number], string>
  dark: Record<(typeof THEME_PALETTE_TOKENS)[number], string>
  /** 概览卡、节点卡和详情面板的表面层级。 */
  surface?: ThemeSurface
  /** 首页顶部概览的布局与统计项。 */
  summary?: ThemeSummaryDefinition
  /** 公开页面外壳背景、页头和页脚。 */
  shell?: ThemeShellDefinition
  /** 公开列表展示的资源行及其顺序；省略等于全部三项。 */
  resources?: ThemeResource[]
  /** 内容开关：省略等于显示，作者显式 false 才隐藏。 */
  show_network?: boolean
  show_host?: boolean
  show_tags?: boolean
  show_remark?: boolean
  /** 公开页字体栈；省略等于产品默认字体，只接受内置固定取值。 */
  font?: ThemeFont
}
/** 站点配置：能写哪些键由主题清单声明的 configuration schema 决定；缺省键沿用主题包自己的值。 */
export type ThemeConfiguration = Partial<Pick<
  ThemeDefinition,
  "overview" | "detail" | "density" | "radius" | "font" | "show_network" | "show_host" | "show_tags" | "show_remark"
>> & Partial<{
  surface: ThemeSurface
  summary_layout: ThemeSummaryLayout
  shell_background: ThemeShellBackground
  shell_header: ThemeShellHeader
  shell_footer: ThemeShellFooter
}>
/** 配置字段的类型由服务端按键推导；作者只声明键、名字与可选取值收窄。 */
export type ThemeConfigurationFieldType = "select" | "number" | "switch"
export interface ThemeConfigurationField {
  key: string
  name?: string
  type: ThemeConfigurationFieldType
  options?: string[]
}
export interface ThemeConfigurationSchema {
  type: "managed"
  items: ThemeConfigurationField[]
}
