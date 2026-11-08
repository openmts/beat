import { describe, expect, it } from "vitest"
import type { ThemeConfigurationSchema, ThemeDefinition } from "@/types/theme-definition"
import {
  applyThemeConfiguration, configurationFields, configurationFieldLabel, configurationFieldValue, configurationFromDefinition,
  configurationOptionLabelKey, configurationPatch, legacyConfigurationFields,
} from "@/lib/theme-configuration-schema"

const definition = {
  schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6,
  light: {}, dark: {}, show_host: false, font: "mono",
} as unknown as ThemeDefinition

const schema: ThemeConfigurationSchema = {
  type: "managed",
  items: [
    { key: "overview", name: "Card layout", type: "select", options: ["cards", "table"] },
    { key: "font", type: "select", options: ["system", "mono"] },
    { key: "radius", type: "number" },
    { key: "show_host", type: "switch" },
    { key: "show_tags", type: "switch" },
  ],
}

describe("theme configuration schema helpers", () => {
  it("falls back to the historical four fields without a declared schema", () => {
    expect(configurationFields(undefined)).toEqual(legacyConfigurationFields)
    expect(configurationFields({ type: "managed", items: [] })).toEqual(legacyConfigurationFields)
    expect(configurationFields(schema)).toEqual(schema.items)
  })

  it("labels author text, built-in keys and option values", () => {
    expect(configurationFieldLabel({ key: "overview", name: "Card layout", type: "select" })).toEqual({ text: "Card layout" })
    expect(configurationFieldLabel({ key: "show_host", type: "switch" })).toEqual({ key: "theme.show_host" })
    expect(configurationFieldLabel({ key: "show_host", name: "theme.show_host", type: "switch" })).toEqual({ key: "theme.show_host" })
    expect(configurationFieldLabel({ key: "radius", type: "number" })).toEqual({ key: "theme.radius" })
    expect(configurationOptionLabelKey("font", "mono")).toBe("theme.font_mono")
    expect(configurationOptionLabelKey("overview", "table")).toBe("theme.table")
  })

  it("reads current values with switch and number fallbacks", () => {
    expect(configurationFieldValue(definition, { key: "overview", type: "select" })).toBe("cards")
    expect(configurationFieldValue(definition, { key: "radius", type: "number" })).toBe(6)
    expect(configurationFieldValue(definition, { key: "show_host", type: "switch" })).toBe(false)
    expect(configurationFieldValue(definition, { key: "show_tags", type: "switch" })).toBe(true)
    expect(configurationFieldValue({} as ThemeDefinition, { key: "radius", type: "number" })).toBe(0)
    expect(configurationFieldValue({} as ThemeDefinition, { key: "overview", type: "select" })).toBe("")
  })

  it("reads the nested summary, surface and shell values exposed to site configuration", () => {
    const extended = {
      ...definition,
      surface: "soft",
      summary: { layout: "focus", metrics: ["availability"] },
      shell: { background: "wash", header: "glass", footer: "inset" },
    } as ThemeDefinition
    expect(configurationFieldValue(extended, { key: "surface", type: "select" })).toBe("soft")
    expect(configurationFieldValue(extended, { key: "summary_layout", type: "select" })).toBe("focus")
    expect(configurationFieldValue(extended, { key: "shell_background", type: "select" })).toBe("wash")
    expect(configurationFieldValue(extended, { key: "shell_header", type: "select" })).toBe("glass")
    expect(configurationFieldValue(extended, { key: "shell_footer", type: "select" })).toBe("inset")
    expect(configurationFieldValue({} as ThemeDefinition, { key: "surface", type: "select" })).toBe("solid")
    expect(configurationFieldValue({} as ThemeDefinition, { key: "summary_layout", type: "select" })).toBe("cards")
    expect(configurationFieldValue({} as ThemeDefinition, { key: "shell_background", type: "select" })).toBe("plain")
  })

  it("builds save payloads from the declared fields only", () => {
    expect(configurationPatch({ key: "show_host", type: "switch" }, false)).toEqual({ show_host: false })
    expect(configurationFromDefinition(definition, schema)).toEqual({
      overview: "cards", font: "mono", radius: 6, show_host: false, show_tags: true,
    })
    expect(configurationFromDefinition(definition, undefined)).toEqual({
      overview: "cards", detail: "balanced", density: "comfortable", radius: 6,
    })
  })

  it("applies flat configuration keys back to the nested definition without mutating the source", () => {
    const source = {
      ...definition,
      surface: "soft",
      summary: { layout: "focus", metrics: ["availability", "traffic"] },
      shell: { background: "wash", header: "glass", footer: "plain" },
    } as ThemeDefinition
    const resolved = applyThemeConfiguration(source, {
      overview: "table", surface: "outline", summary_layout: "strip",
      shell_background: "grid", shell_header: "solid", shell_footer: "minimal",
    })
    expect(resolved).toMatchObject({
      overview: "table", surface: "outline",
      summary: { layout: "strip", metrics: ["availability", "traffic"] },
      shell: { background: "grid", header: "solid", footer: "minimal" },
    })
    expect(source).toMatchObject({
      overview: "cards", surface: "soft",
      summary: { layout: "focus" },
      shell: { background: "wash", header: "glass", footer: "plain" },
    })
  })

  it("preserves unspecified nested values when applying a partial configuration", () => {
    const source = {
      ...definition,
      summary: { layout: "focus", metrics: ["availability"] },
      shell: { background: "wash", header: "glass", footer: "inset" },
    } as ThemeDefinition
    expect(applyThemeConfiguration(source, { shell_header: "transparent" }).shell).toEqual({ background: "wash", header: "transparent", footer: "inset" })
    expect(applyThemeConfiguration(source, {}).summary).toEqual(source.summary)
  })
})
