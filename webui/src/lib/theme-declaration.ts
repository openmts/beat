// 主题包声明摘要：把包里的 theme.json（声明式定义）整理成后台能直接展示的几组信息。
// 纯展示逻辑，不参与校验；服务端仍然是权威。

import {
  configurationFieldLabel, configurationOptionLabelKey, themeRadiusMaximum, themeRadiusMinimum,
} from "@/lib/theme-configuration-schema"
import { THEME_PALETTE_TOKENS, type ThemeDefinition, type ThemeConfigurationField, type ThemeConfigurationSchema, type ThemeResource } from "@/types/theme-definition"

/** 后台翻译函数：声明摘要只负责拼文案，具体语言由调用方决定。 */
type Translator = (key: string, params?: Record<string, string | number>) => string

/** 内容开关：省略等于显示，只有作者显式写 false 才算隐藏。 */
export const declarationSwitchKeys = ["show_network", "show_host", "show_tags", "show_remark"] as const
export type DeclarationSwitchKey = (typeof declarationSwitchKeys)[number]

/** 默认资源顺序与服务端一致；作者声明 resources 时按声明顺序返回。 */
export function declarationResources(definition: ThemeDefinition): ThemeResource[] {
  return definition.resources && definition.resources.length > 0 ? definition.resources : ["cpu", "memory", "disk"]
}

/** hiddenSwitches 返回被作者显式关闭的内容项，用于告诉运维「这个主题会少显示什么」。 */
export function hiddenSwitches(definition: ThemeDefinition): DeclarationSwitchKey[] {
  return declarationSwitchKeys.filter((key) => definition[key] === false)
}

/** paletteEntries 按固定顺序给出 24 个调色板字段，顺序与服务端字段表一致，便于逐项对照。 */
export function paletteEntries(definition: ThemeDefinition, mode: "light" | "dark"): [string, string][] {
  const palette = definition[mode]
  return THEME_PALETTE_TOKENS.map((token) => [token, palette[token]])
}

/**
 * configurationSummary 列出主题声明给站点调整的字段与可选取值，供「包声明摘要」在激活前说明这个包
 * 会开放什么。没有声明 configuration 的包给出历史四字段说明，与后台预览面板的兜底口径一致。
 */
export function configurationSummary(schema: ThemeConfigurationSchema | undefined, t: Translator): string[] {
  const items = schema?.items ?? []
  if (items.length === 0) return [t("theme.configurable_legacy")]
  return items.map((item) => `${configurationLabel(item, t)}: ${configurationValues(item, t)}`)
}

function configurationLabel(item: ThemeConfigurationField, t: Translator): string {
  const label = configurationFieldLabel(item)
  return "text" in label ? label.text : t(label.key)
}

function configurationValues(item: ThemeConfigurationField, t: Translator): string {
  if (item.type === "select") return (item.options ?? []).map((option) => t(configurationOptionLabelKey(item.key, option))).join(" / ")
  if (item.type === "switch") return t("theme.configurable_switch")
  return t("theme.configurable_number", { min: themeRadiusMinimum, max: themeRadiusMaximum })
}

/** 包体积按二进制单位展示；小于 1 KiB 时按字节，避免出现 0.0 KiB。 */
export function packageSizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}
