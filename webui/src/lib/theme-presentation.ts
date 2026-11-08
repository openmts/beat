import { createContext, useContext, type CSSProperties } from "react"
import {
  THEME_FONT_TOKENS, THEME_PALETTE_TOKENS, THEME_RESOURCE_TOKENS, THEME_SHELL_BACKGROUND_TOKENS,
  THEME_SHELL_FOOTER_TOKENS, THEME_SHELL_HEADER_TOKENS, THEME_SUMMARY_LAYOUT_TOKENS, THEME_SUMMARY_METRIC_TOKENS,
  THEME_SURFACE_TOKENS, type ThemeDefinition, type ThemeFont, type ThemeResource, type ThemeShellBackground,
  type ThemeShellFooter, type ThemeShellHeader, type ThemeSummaryLayout, type ThemeSummaryMetric, type ThemeSurface,
} from "@/types/theme-definition"
import type { ThemeSnapshot } from "@/types/theme-runtime"

export interface PublicThemePresentation {
  definition: ThemeDefinition
  snapshot: ThemeSnapshot
  onNodeSelect?: (id: string) => void
}
export const PublicThemeContext = createContext<PublicThemePresentation | null>(null)
export function usePublicTheme() { return useContext(PublicThemeContext) }

/** 列表渲染用的资源行：作者留空时按默认顺序展示全部资源。 */
export function themeResources(definition: ThemeDefinition): ThemeResource[] {
  return definition.resources?.length ? definition.resources : [...THEME_RESOURCE_TOKENS]
}

export const DEFAULT_THEME_SUMMARY_METRICS: ThemeSummaryMetric[] = ["availability", "busiest", "traffic", "throughput"]

export interface ResolvedThemeSummary {
  layout: ThemeSummaryLayout
  metrics: ThemeSummaryMetric[]
}

export interface ResolvedThemeShell {
  background: ThemeShellBackground
  header: ThemeShellHeader
  footer: ThemeShellFooter
}

export function themeSummary(definition: ThemeDefinition): ResolvedThemeSummary {
  return {
    layout: definition.summary?.layout ?? "cards",
    metrics: definition.summary?.metrics?.length ? [...definition.summary.metrics] : [...DEFAULT_THEME_SUMMARY_METRICS],
  }
}

export function themeShell(definition: ThemeDefinition): ResolvedThemeShell {
  return {
    background: definition.shell?.background ?? "plain",
    header: definition.shell?.header ?? "glass",
    footer: definition.shell?.footer ?? "plain",
  }
}

const themeSurfaceClasses: Record<ThemeSurface, string> = {
  solid: "border border-border/80 bg-card shadow-sm ring-0",
  soft: "border border-border/60 bg-card/70 shadow-xs ring-0 backdrop-blur-sm",
  outline: "border border-border bg-transparent shadow-none ring-0",
  flat: "border border-transparent bg-transparent shadow-none ring-0",
}

/** 主题控制的表面样式；调用方只追加圆角、间距等布局类。 */
export function themeSurfaceClass(definition: ThemeDefinition): string {
  return themeSurfaceClasses[definition.surface ?? "solid"]
}

/**
 * 作者可选字体只在渲染器内置的固定字体栈里取值：不进网络字体、不读系统外的资源，
 * 省略该字段时保留产品默认字体（Geist），显式 `system` 才切到系统 UI 字体。
 */
const themeFontStacks: Record<ThemeFont, string> = {
  system: 'ui-sans-serif, system-ui, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  serif: 'ui-serif, Georgia, Cambria, "Times New Roman", "Songti SC", serif',
  mono: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "DejaVu Sans Mono", monospace',
}

export function themeFontFamily(definition: ThemeDefinition): string | undefined {
  return definition.font ? themeFontStacks[definition.font] : undefined
}

export type ThemeContentFlag = "show_network" | "show_host" | "show_tags" | "show_remark"

/** 内容开关缺省为显示：只有作者显式写 false 才隐藏。 */
export function themeFlag(definition: ThemeDefinition, flag: ThemeContentFlag): boolean {
  return definition[flag] !== false
}

function validThemeLayout(definition: ThemeDefinition): boolean {
  return definition.schema_version === 1 && ["cards", "table", "tiles", "list"].includes(definition.overview) &&
    ["balanced", "compact", "spotlight"].includes(definition.detail) && ["comfortable", "compact"].includes(definition.density)
}

/** 资源行必须是已知取值、互不重复，且最多与全集等长；越界定义按无效主题处理。 */
function validThemeContent(definition: ThemeDefinition): boolean {
  if (definition.resources !== undefined) {
    if (!Array.isArray(definition.resources) || definition.resources.length === 0 || definition.resources.length > THEME_RESOURCE_TOKENS.length) return false
    if (new Set(definition.resources).size !== definition.resources.length) return false
    if (!definition.resources.every((resource) => THEME_RESOURCE_TOKENS.includes(resource))) return false
  }
  if (!(["show_network", "show_host", "show_tags", "show_remark"] as const).every((flag) => definition[flag] === undefined || typeof definition[flag] === "boolean")) return false
  if (definition.font !== undefined && !THEME_FONT_TOKENS.includes(definition.font)) return false
  if (definition.surface !== undefined && !THEME_SURFACE_TOKENS.includes(definition.surface)) return false
  if (definition.summary !== undefined) {
    if (definition.summary.layout !== undefined && !THEME_SUMMARY_LAYOUT_TOKENS.includes(definition.summary.layout)) return false
    const metrics = definition.summary.metrics
    if (metrics !== undefined) {
      if (!Array.isArray(metrics) || metrics.length > THEME_SUMMARY_METRIC_TOKENS.length || new Set(metrics).size !== metrics.length) return false
      if (!metrics.every((metric) => THEME_SUMMARY_METRIC_TOKENS.includes(metric))) return false
    }
  }
  if (definition.shell !== undefined) {
    if (definition.shell.background !== undefined && !THEME_SHELL_BACKGROUND_TOKENS.includes(definition.shell.background)) return false
    if (definition.shell.header !== undefined && !THEME_SHELL_HEADER_TOKENS.includes(definition.shell.header)) return false
    if (definition.shell.footer !== undefined && !THEME_SHELL_FOOTER_TOKENS.includes(definition.shell.footer)) return false
  }
  return true
}

export function publicThemeStyles(definition: ThemeDefinition, mode: "light" | "dark"): CSSProperties {
  if (!validThemeLayout(definition) || !validThemeContent(definition) ||
    !Number.isInteger(definition.radius) || definition.radius < 0 || definition.radius > 8) throw new Error("Invalid declarative theme")
  const palette = definition[mode]
  if (!palette || Object.keys(palette).length !== THEME_PALETTE_TOKENS.length) throw new Error("Invalid theme palette")
  const family = themeFontFamily(definition)
  const variables: Record<string, string> = family ? { "--radius": `${definition.radius}px`, fontFamily: family } : { "--radius": `${definition.radius}px` }
  for (const token of THEME_PALETTE_TOKENS) {
    const color = palette[token]
    if (!/^#[a-fA-F0-9]{6}$/.test(color)) throw new Error("Invalid theme color")
    variables[`--${token}`] = color
    variables[`--color-${token}`] = color
  }
  return variables as CSSProperties
}
