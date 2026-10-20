import { createContext, useContext, type CSSProperties } from "react"
import { THEME_FONT_TOKENS, THEME_PALETTE_TOKENS, THEME_RESOURCE_TOKENS, type ThemeDefinition, type ThemeFont, type ThemeResource } from "@/types/theme-definition"
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
  return definition.font === undefined || THEME_FONT_TOKENS.includes(definition.font)
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
