import { describe, expect, it } from "vitest"
import type { ThemeConfigurationSchema, ThemeDefinition } from "@/types/theme-definition"
import {
  configurationFields, configurationFieldLabel, configurationFieldValue, configurationFromDefinition,
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

  it("builds save payloads from the declared fields only", () => {
    expect(configurationPatch({ key: "show_host", type: "switch" }, false)).toEqual({ show_host: false })
    expect(configurationFromDefinition(definition, schema)).toEqual({
      overview: "cards", font: "mono", radius: 6, show_host: false, show_tags: true,
    })
    expect(configurationFromDefinition(definition, undefined)).toEqual({
      overview: "cards", detail: "balanced", density: "comfortable", radius: 6,
    })
  })
})
