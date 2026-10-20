export const THEME_PALETTE_TOKENS = [
  "background", "foreground", "card", "card-foreground", "popover", "popover-foreground", "primary", "primary-foreground",
  "secondary", "secondary-foreground", "muted", "muted-foreground", "accent", "accent-foreground", "destructive", "destructive-foreground",
  "border", "input", "ring", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
] as const
export const THEME_RESOURCE_TOKENS = ["cpu", "memory", "disk"] as const
export type ThemeResource = (typeof THEME_RESOURCE_TOKENS)[number]
export const THEME_FONT_TOKENS = ["system", "serif", "mono"] as const
export type ThemeFont = (typeof THEME_FONT_TOKENS)[number]

export interface ThemeDefinition {
  schema_version: 1
  overview: "cards" | "table" | "tiles" | "list"
  detail: "balanced" | "compact" | "spotlight"
  density: "comfortable" | "compact"
  radius: number
  light: Record<(typeof THEME_PALETTE_TOKENS)[number], string>
  dark: Record<(typeof THEME_PALETTE_TOKENS)[number], string>
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
>>
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
