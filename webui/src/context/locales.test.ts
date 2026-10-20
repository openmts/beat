import { describe, expect, it } from "vitest"
import {
  browserLocaleCandidates,
  initialLocale,
  catalogGaps,
  fallbackLocales,
  localeOptions,
  prefixedTranslator,
  readStoredLocale,
  resolveLocale,
  shippedLocales,
  writeStoredLocale,
  type Catalog,
  type LocaleStorage,
} from "@/context/locales"

function storage(initial: Record<string, string> = {}): LocaleStorage & { values: Record<string, string> } {
  const values = { ...initial }
  return { values, getItem: (key) => values[key] ?? null, setItem: (key, value) => { values[key] = value } }
}

describe("locale resolution", () => {
  it("prefers an exact match before the language family", () => {
    expect(resolveLocale(["zh-TW", "zh-CN"])).toBe("zh-TW")
    expect(resolveLocale(["en-GB"])).toBe("en")
  })

  it("maps a language family onto the shipped writing system", () => {
    expect(resolveLocale(["zh"])).toBe("zh-CN")
    expect(resolveLocale(["zh-Hans-CN"])).toBe("zh-CN")
    expect(resolveLocale(["zh-Hant"])).toBe("zh-TW")
    expect(resolveLocale(["en_US"])).toBe("en")
  })

  it("falls back to English for unsupported languages", () => {
    expect(resolveLocale(["ja-JP"])).toBe("ja")
    expect(resolveLocale(["id-ID"])).toBe("id")
    expect(resolveLocale(["in-ID"])).toBe("id")
    expect(resolveLocale(["fr-FR", "id-ID"])).toBe("id")
    expect(resolveLocale([])).toBe("en")
    expect(resolveLocale(["  "])).toBe("en")
  })

  it("resolves traditional Chinese once its catalog ships", () => {
    expect(resolveLocale(["zh-Hant-TW"], ["en", "zh-CN", "zh-TW"])).toBe("zh-TW")
    expect(resolveLocale(["zh-CN"], ["en", "zh-TW"])).toBe("zh-TW")
  })

  it("falls back from traditional to simplified before English", () => {
    expect(fallbackLocales("zh-TW")).toEqual(["zh-CN", "en"])
    expect(fallbackLocales("zh-CN")).toEqual(["en"])
    expect(fallbackLocales("ja")).toEqual(["en"])
  })
})

describe("locale persistence and browser candidates", () => {
  it("reads only shipped locales and ignores everything else", () => {
    expect(readStoredLocale(storage({ locale: "zh-CN" }))).toBe("zh-CN")
    expect(readStoredLocale(storage({ locale: "ja" }))).toBe("ja")
    expect(readStoredLocale(storage({ locale: "id" }))).toBe("id")
    expect(readStoredLocale(storage({ locale: "fr" }))).toBeUndefined()
    expect(readStoredLocale(storage())).toBeUndefined()
    expect(readStoredLocale(undefined)).toBeUndefined()
  })

  it("survives storage denial on read and write", () => {
    const denied: LocaleStorage = {
      getItem: () => { throw new Error("denied") },
      setItem: () => { throw new Error("denied") },
    }
    expect(readStoredLocale(denied)).toBeUndefined()
    expect(() => writeStoredLocale(denied, "zh-CN")).not.toThrow()
    expect(() => writeStoredLocale(undefined, "zh-CN")).not.toThrow()
  })

  it("persists the chosen locale", () => {
    const target = storage()
    writeStoredLocale(target, "zh-CN")
    expect(target.values.locale).toBe("zh-CN")
  })

  it("prefers the saved choice, then the browser, then English", () => {
    expect(initialLocale(storage({ locale: "zh-CN" }), { language: "en-US" })).toBe("zh-CN")
    expect(initialLocale(storage({ locale: "fr" }), { languages: ["zh-CN", "en"] })).toBe("zh-CN")
    expect(initialLocale(undefined, undefined)).toBe("en")
  })

  it("orders browser candidates from the most specific to the least", () => {
    expect(browserLocaleCandidates({ languages: ["ja-JP", "en-US"], language: "en" })).toEqual(["ja-JP", "en-US"])
    expect(browserLocaleCandidates({ language: "zh-CN" })).toEqual(["zh-CN"])
    expect(browserLocaleCandidates({ languages: [] })).toEqual([])
    expect(browserLocaleCandidates(undefined)).toEqual([])
  })
})

describe("catalog completeness", () => {
  it("lists every shipped locale in the menu metadata and no locale twice", () => {
    const codes = localeOptions.map((option) => option.code)
    expect(new Set(codes).size).toBe(codes.length)
    for (const locale of shippedLocales) expect(codes).toContain(locale)
  })

  it("reports the keys a locale is missing", () => {
    const catalogs: Catalog[] = [
      { en: { a: "A", b: "B" }, "zh-CN": { a: "甲" } },
      { en: { c: "C" }, "zh-CN": { c: "丙" } },
    ]
    expect(catalogGaps(catalogs, "zh-CN")).toEqual(["b"])
    expect(catalogGaps(catalogs, "en")).toEqual([])
  })
})

describe("prefixedTranslator", () => {
  const t = (key: string, params?: Record<string, string | number>) =>
    key === "fleet.title" ? `Fleet ${params?.name ?? ""}`.trim() : key

  it("localizes known keys and passes unknown values through unchanged", () => {
    const translate = prefixedTranslator(t, "fleet")
    expect(translate("title")).toBe("Fleet")
    expect(translate("storage offline")).toBe("storage offline")
  })

  it("forwards parameters to the catalog lookup", () => {
    expect(prefixedTranslator(t, "fleet")("title", { name: "alpha" })).toBe("Fleet alpha")
  })
})
