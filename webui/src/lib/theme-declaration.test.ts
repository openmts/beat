import { describe, expect, it } from "vitest"
import { configurationSummary, declarationResources, hiddenSwitches, packageSizeLabel, paletteEntries } from "@/lib/theme-declaration"
import { THEME_PALETTE_TOKENS, type ThemeConfigurationSchema, type ThemeDefinition } from "@/types/theme-definition"

/** 测试用翻译函数：内置键给短标签，占位符按与产品一致的规则替换。 */
function translator(keys: Record<string, string>) {
  return (key: string, params?: Record<string, string | number>) => {
    const template = keys[key] ?? key
    if (!params) return template
    return template.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name]))
  }
}

const palette = () => Object.fromEntries(THEME_PALETTE_TOKENS.map((token) => [token, "#123456"])) as ThemeDefinition["light"]

const definition = (patch: Partial<ThemeDefinition> = {}): ThemeDefinition => ({
  schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 4,
  light: palette(), dark: palette(), ...patch,
})

describe("theme declaration summary", () => {
  it("falls back to the server's default resource order", () => {
    expect(declarationResources(definition())).toEqual(["cpu", "memory", "disk"])
    expect(declarationResources(definition({ resources: ["memory"] }))).toEqual(["memory"])
  })

  it("lists only the switches the author turned off", () => {
    expect(hiddenSwitches(definition())).toEqual([])
    expect(hiddenSwitches(definition({ show_network: true, show_host: false, show_tags: false }))).toEqual(["show_host", "show_tags"])
  })

  it("keeps the palette in the server field order", () => {
    expect(paletteEntries(definition(), "light").map(([token]) => token)).toEqual([...THEME_PALETTE_TOKENS])
    expect(paletteEntries(definition(), "dark")).toHaveLength(24)
  })

  it("lists the declared site-configurable fields with their values", () => {
    const schema: ThemeConfigurationSchema = { type: "managed", items: [
      { key: "overview", name: "Card layout", type: "select", options: ["cards", "table"] },
      { key: "font", type: "select", options: ["mono"] },
      { key: "radius", type: "number" },
      { key: "show_tags", type: "switch" },
    ] }
    expect(configurationSummary(schema, translator({
      "theme.overview_layout": "Overview layout", "theme.font": "Font", "theme.radius": "Corner radius (px)", "theme.show_tags": "Node tags",
      "theme.cards": "Grouped cards", "theme.table": "Fleet table", "theme.font_mono": "Monospace",
      "theme.configurable_number": "whole number {min}\u2013{max}", "theme.configurable_switch": "on / off",
    }))).toEqual([
      "Card layout: Grouped cards / Fleet table",
      "Font: Monospace",
      "Corner radius (px): whole number 0\u20138",
      "Node tags: on / off",
    ])
  })

  it("falls back to the historical four fields when the package declares none", () => {
    const t = translator({ "theme.configurable_legacy": "Overview layout, detail layout, density and corner radius" })
    expect(configurationSummary(undefined, t)).toEqual(["Overview layout, detail layout, density and corner radius"])
    expect(configurationSummary({ type: "managed", items: [] }, t)).toEqual(["Overview layout, detail layout, density and corner radius"])
  })

  it("formats package sizes with binary units", () => {
    expect(packageSizeLabel(0)).toBe("0 B")
    expect(packageSizeLabel(512)).toBe("512 B")
    expect(packageSizeLabel(2048)).toBe("2.0 KiB")
    expect(packageSizeLabel(1024 * 1024 * 3.5)).toBe("3.5 MiB")
  })
})
