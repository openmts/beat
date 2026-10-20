import type { CSSProperties } from "react"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 与服务端 dashboardStat* 一致：Color mode 决定阈值色落在数值还是面板底色上，
 *  Text mode 决定主文本显示什么，Text size 决定主文本字号档位，
 *  Orientation 决定名称与数值的排版方向。 */
export const statColorModes = ["value", "background", "none"] as const
export const statTextModes = ["auto", "value", "name", "none"] as const
export const statTextSizes = ["auto", "sm", "md", "lg", "xl"] as const
export const statOrientations = ["auto", "horizontal", "vertical"] as const
export type StatColorMode = typeof statColorModes[number]
export type StatTextMode = typeof statTextModes[number]
export type StatTextSize = typeof statTextSizes[number]
export type StatOrientation = typeof statOrientations[number]

export type StatStyleKey = "stat_color_mode" | "stat_text_mode" | "stat_text_size" | "stat_orientation"
export type StatStylePatch = Pick<DashboardPanel, StatStyleKey>
export type StatStyleFields = Pick<DashboardPanel, "type" | StatStyleKey>

/** auto 沿用改动前的 text-4xl，其余档位只是换一档 Tailwind 字号。 */
const statTextSizeClasses: Record<StatTextSize, string> = {
  auto: "text-4xl", sm: "text-2xl", md: "text-3xl", lg: "text-4xl", xl: "text-5xl",
}

const statStyleValues: Record<StatStyleKey, readonly string[]> = {
  stat_color_mode: statColorModes, stat_text_mode: statTextModes, stat_text_size: statTextSizes,
  stat_orientation: statOrientations,
}
const statStyleDefaults: Record<StatStyleKey, string> = {
  stat_color_mode: "value", stat_text_mode: "auto", stat_text_size: "auto", stat_orientation: "auto",
}

/** 取值面板外观：未知或缺省都退回服务端的缺省，渲染器不再判断。 */
export function panelStatStyle(panel: Pick<DashboardPanel, StatStyleKey>): { colorMode: StatColorMode; textMode: StatTextMode; textSize: StatTextSize; orientation: StatOrientation } {
  return {
    colorMode: statColorModes.find((mode) => mode === panel.stat_color_mode) ?? "value",
    textMode: statTextModes.find((mode) => mode === panel.stat_text_mode) ?? "auto",
    textSize: statTextSizes.find((size) => size === panel.stat_text_size) ?? "auto",
    orientation: statOrientations.find((value) => value === panel.stat_orientation) ?? "auto",
  }
}

/** statOrientationLayout 把排版方向翻译成 DOM 结构：auto 与 vertical 都把名称单独放一行
 *  （auto 沿用改动前的「数值在上、名称在下」，vertical 是 Grafana 的「名称在上、数值在下」），
 *  horizontal 让名称与数值同行。渲染器据此拼装，不再重复判断取值。 */
export function statOrientationLayout(orientation: StatOrientation): "stacked-below" | "stacked-above" | "inline" {
  if (orientation === "horizontal") return "inline"
  if (orientation === "vertical") return "stacked-above"
  return "stacked-below"
}

/** statTextSizeClass 把档位换成渲染用的 Tailwind 字号；未知档位退回 auto。 */
export function statTextSizeClass(size: StatTextSize): string {
  return statTextSizeClasses[size] ?? statTextSizeClasses.auto
}

/** statStyleError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardStatOptions 一致。 */
export function statStyleError(panel: StatStyleFields): string | null {
  if (panel.stat_color_mode === undefined && panel.stat_text_mode === undefined &&
    panel.stat_text_size === undefined && panel.stat_orientation === undefined) return null
  if (panel.type !== "stat") return "workspace.stat_style_stat_only"
  for (const key of Object.keys(statStyleValues) as StatStyleKey[]) {
    const value = panel[key]
    if (value !== undefined && !statStyleValues[key].some((candidate) => candidate === value)) return `workspace.${key}_invalid`
  }
  return null
}

/** statStylePatch 切到非 stat 面板时清掉这四个字段，避免保存出服务端会拒绝的组合。 */
export function statStylePatch(type: string): StatStylePatch {
  return type === "stat" ? {} : { stat_color_mode: undefined, stat_text_mode: undefined, stat_text_size: undefined, stat_orientation: undefined }
}

/** statPatch 把选项归一成补丁：选回缺省值时省略字段，配置文件里只留非默认项。 */
export function statPatch(key: StatStyleKey, value: string): StatStylePatch {
  const known = statStyleValues[key].some((candidate) => candidate === value)
  return known && value !== statStyleDefaults[key] ? { [key]: value } : { [key]: undefined }
}

/** statAppearanceStyles 生成外观样式：value 给数值上色，background 给面板底色并按亮度选前景，
 *  none 完全不着色（图表与迷你趋势图回退到当前文字色）。 */
export function statAppearanceStyles(panel: StatStyleFields, color: string): { container?: CSSProperties; value?: CSSProperties; accent: string } {
  const mode = panelStatStyle(panel).colorMode
  if (mode === "none") return { accent: "currentColor" }
  if (mode === "background") {
    const foreground = readableStatForeground(color)
    return { container: { backgroundColor: color, color: foreground }, accent: foreground }
  }
  return { value: { color }, accent: color }
}

/** readableStatForeground 与表格的单元格着色共用同一套相对亮度规则，保证底色上的文字可读。 */
export function readableStatForeground(hex: string): string {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return "#f8fafc"
  const [red, green, blue] = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16))
  const luminance = (0.2126 * red + 0.7152 * green + 0.0722 * blue) / 255
  return luminance > 0.6 ? "#0f172a" : "#f8fafc"
}
