// 主题自声明配置（清单 configuration：managed）的前端口径。服务端已经校验过清单，这里是纯展示与
// 草稿逻辑：按字段类型渲染控件、按当前生效定义预填值、按打包字段生成提交体。

import type {
  ThemeConfiguration,
  ThemeConfigurationField,
  ThemeConfigurationSchema,
  ThemeDefinition,
} from "@/types/theme-definition"

/** 圆角半径的合法区间；与服务端 `model.ThemeRadiusMinimum/Maximum` 一致。 */
export const themeRadiusMinimum = 0
export const themeRadiusMaximum = 8

/** 没有声明 configuration 的主题继续使用历史上的四个显示字段。 */
export const legacyConfigurationFields: ThemeConfigurationField[] = [
  { key: "overview", type: "select", options: ["cards", "table", "tiles", "list"] },
  { key: "detail", type: "select", options: ["balanced", "compact", "spotlight"] },
  { key: "density", type: "select", options: ["comfortable", "compact"] },
  { key: "radius", type: "number" },
]

/** 键到内置文案的兜底映射；作者没写 name 时用它生成表单标题。 */
const defaultLabelKeys: Record<string, string> = {
  overview: "theme.overview_layout",
  detail: "theme.detail_layout",
  density: "theme.density",
  radius: "theme.radius",
  font: "theme.font",
  surface: "theme.surface",
  summary_layout: "theme.summary_layout",
  shell_background: "theme.shell_background",
  shell_header: "theme.shell_header",
  shell_footer: "theme.shell_footer",
  show_network: "theme.show_network",
  show_host: "theme.show_host",
  show_tags: "theme.show_tags",
  show_remark: "theme.show_remark",
}

/** 作者写的 name 以 `theme.` 开头时按内置文案键解释，否则按纯文本展示。 */
export function configurationFieldLabel(field: ThemeConfigurationField): { key: string } | { text: string } {
  if (field.name && !field.name.startsWith("theme.")) return { text: field.name }
  return { key: field.name ?? defaultLabelKeys[field.key] ?? field.key }
}

/** 取值标签：字体族用 `theme.font_*`，其它枚举沿用 `theme.<取值>`。 */
export function configurationOptionLabelKey(key: string, value: string): string {
  return key === "font" ? `theme.font_${value}` : `theme.${value}`
}

export function configurationFields(schema?: ThemeConfigurationSchema): ThemeConfigurationField[] {
  return schema && schema.items.length > 0 ? schema.items : legacyConfigurationFields
}

function definitionValue(definition: ThemeDefinition, key: string): unknown {
  if (key === "surface") return definition.surface ?? "solid"
  if (key === "summary_layout") return definition.summary?.layout ?? "cards"
  if (key === "shell_background") return definition.shell?.background ?? "plain"
  if (key === "shell_header") return definition.shell?.header ?? "glass"
  if (key === "shell_footer") return definition.shell?.footer ?? "plain"
  return (definition as unknown as Record<string, unknown>)[key]
}

/** 表单展示值：开关缺省为开，数字缺省 0，枚举缺省取主题自己的声明。 */
export function configurationFieldValue(
  definition: ThemeDefinition,
  field: ThemeConfigurationField,
): string | number | boolean {
  const value = definitionValue(definition, field.key)
  if (field.type === "switch") return value !== false
  if (field.type === "number") return typeof value === "number" ? value : 0
  return typeof value === "string" ? value : ""
}

/** 单字段变更体；键来自服务端下发的 schema，不会写出主题没声明的字段。 */
export function configurationPatch(
  field: ThemeConfigurationField,
  value: string | number | boolean,
): ThemeConfiguration {
  return { [field.key]: value } as ThemeConfiguration
}

/** 保存时提交的完整配置：声明式主题按 schema 逐项写入当前生效值，历史主题仍是四个显示字段。 */
export function configurationFromDefinition(
  definition: ThemeDefinition,
  schema?: ThemeConfigurationSchema,
): ThemeConfiguration {
  return configurationFields(schema).reduce<ThemeConfiguration>(
    (draft, field) => ({ ...draft, ...configurationPatch(field, configurationFieldValue(definition, field)) }),
    {},
  )
}

/**
 * 把站点配置合并回主题定义。`summary_layout` 与 `shell_*` 是配置表单的扁平键，
 * 渲染器读取的是嵌套的 `summary` / `shell`，预览和保存都必须走同一条转换。
 */
export function applyThemeConfiguration(
  definition: ThemeDefinition,
  configuration: ThemeConfiguration,
): ThemeDefinition {
  const resolved: ThemeDefinition = { ...definition }
  if (configuration.overview !== undefined) resolved.overview = configuration.overview
  if (configuration.detail !== undefined) resolved.detail = configuration.detail
  if (configuration.density !== undefined) resolved.density = configuration.density
  if (configuration.radius !== undefined) resolved.radius = configuration.radius
  if (configuration.font !== undefined) resolved.font = configuration.font
  if (configuration.surface !== undefined) resolved.surface = configuration.surface
  if (configuration.show_network !== undefined) resolved.show_network = configuration.show_network
  if (configuration.show_host !== undefined) resolved.show_host = configuration.show_host
  if (configuration.show_tags !== undefined) resolved.show_tags = configuration.show_tags
  if (configuration.show_remark !== undefined) resolved.show_remark = configuration.show_remark
  if (configuration.summary_layout !== undefined) {
    resolved.summary = { ...definition.summary, layout: configuration.summary_layout }
  }
  if (configuration.shell_background !== undefined || configuration.shell_header !== undefined || configuration.shell_footer !== undefined) {
    resolved.shell = {
      ...definition.shell,
      ...(configuration.shell_background !== undefined ? { background: configuration.shell_background } : {}),
      ...(configuration.shell_header !== undefined ? { header: configuration.shell_header } : {}),
      ...(configuration.shell_footer !== undefined ? { footer: configuration.shell_footer } : {}),
    }
  }
  return resolved
}
