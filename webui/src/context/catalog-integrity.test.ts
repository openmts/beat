import { describe, expect, it } from "vitest"
import { catalogGaps, draftLocales, shippedLocales, type Catalog, type Locale } from "@/context/locales"
import { allCatalogs } from "@/test/catalogs"

// 门禁目录：每一个已发布语言都必须覆盖这里全部键，缺一个就不允许发布。
const catalogs: Catalog[] = allCatalogs

const english: Record<string, string> = {}
for (const catalog of catalogs) Object.assign(english, catalog.en ?? {})

function presentLocales(): Locale[] {
  const found = new Set<Locale>()
  for (const catalog of catalogs) for (const locale of Object.keys(catalog) as Locale[]) found.add(locale)
  return [...found]
}

function placeholders(value: string): string[] {
  return [...value.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()
}

function hasControlCharacter(value: string): boolean {
  return [...value].some((character) => {
    const code = character.codePointAt(0) ?? 0
    return code < 9 || (code > 10 && code < 32) || code === 127
  })
}
const dangerousMarkup = /<\s*\/?\s*(script|iframe|style|object|embed|link|meta|img|svg)\b/i
const dangerousScheme = /\b(?:javascript|vbscript|data):/i

describe("catalog integrity", () => {
  it("has a reference entry for every key of every catalog", () => {
    const missing = catalogs.flatMap((catalog) => Object.keys(catalog.en ?? {}).filter((key) => !english[key]))
    expect(missing).toEqual([])
    expect(Object.keys(english).length).toBeGreaterThan(1000)
  })

  it("keeps every shipped locale complete", () => {
    for (const locale of shippedLocales) expect(catalogGaps(catalogs, locale)).toEqual([])
  })

  // 未发布语言只能出现在 draftLocales 白名单里：意外的半成品目录必须让构建失败，
  // 而不是悄悄进入语言菜单。draftLocales 不会出现在菜单、浏览器识别与语言回退中。
  it("keeps unshipped languages on the explicit draft list", () => {
    const allowed = new Set<Locale>([...shippedLocales, ...draftLocales])
    expect(presentLocales().filter((locale) => !allowed.has(locale))).toEqual([])
    expect(draftLocales.filter((locale) => shippedLocales.includes(locale))).toEqual([])
  })

  // 草稿语言补齐后必须移入 shippedLocales：零缺口的草稿会让门禁失败，避免翻译完成却长期不上线。
  it("promotes a complete draft locale", () => {
    const present = presentLocales()
    const complete = draftLocales.filter((locale) => present.includes(locale) && catalogGaps(catalogs, locale).length === 0)
    expect(complete).toEqual([])
  })

  // 草稿语言只要求“不破坏门禁”：键必须是真实英文键、占位符一致、内容有界且安全。
  // 完整性由补齐后再移入 shippedLocales 来保证，因此草稿允许缺键。
  it("validates draft catalogs without demanding completeness", () => {
    const problems: string[] = []
    for (const catalog of catalogs) {
      for (const locale of draftLocales) {
        for (const [key, value] of Object.entries(catalog[locale] ?? {})) {
          const reference = english[key]
          if (reference === undefined) problems.push(`${locale} ${key}: unknown key`)
          if (typeof value !== "string" || value.trim().length === 0) problems.push(`${locale} ${key}: blank or non-string`)
          if (new TextEncoder().encode(value).length > 2000) problems.push(`${locale} ${key}: exceeds 2000 bytes`)
          if (hasControlCharacter(value)) problems.push(`${locale} ${key}: control character`)
          if (dangerousMarkup.test(value) || dangerousScheme.test(value)) problems.push(`${locale} ${key}: unsafe markup`)
          if (value === key) problems.push(`${locale} ${key}: raw key`)
          if (reference !== undefined && JSON.stringify(placeholders(value)) !== JSON.stringify(placeholders(reference))) {
            problems.push(`${locale} ${key}: placeholder mismatch`)
          }
        }
      }
    }
    expect(problems).toEqual([])
  })

  it("keeps every translation typed, non-blank, bounded and placeholder-compatible", () => {
    const problems: string[] = []
    for (const catalog of catalogs) {
      for (const locale of Object.keys(catalog) as Locale[]) {
        for (const [key, value] of Object.entries(catalog[locale] ?? {})) {
          const reference = english[key] ?? ""
          if (typeof value !== "string" || value.trim().length === 0) problems.push(`${locale} ${key}: blank or non-string`)
          if (new TextEncoder().encode(value).length > 2000) problems.push(`${locale} ${key}: exceeds 2000 bytes`)
          if (hasControlCharacter(value)) problems.push(`${locale} ${key}: control character`)
          if (dangerousMarkup.test(value) || dangerousScheme.test(value)) problems.push(`${locale} ${key}: unsafe markup`)
          if (locale !== "en" && JSON.stringify(placeholders(value)) !== JSON.stringify(placeholders(reference))) {
            problems.push(`${locale} ${key}: placeholder mismatch`)
          }
        }
      }
    }
    expect(problems).toEqual([])
  })

  it("uses bounded keys everywhere", () => {
    const problems = Object.keys(english).filter((key) => key.length === 0 || key.length > 120 || /\s/.test(key))
    expect(problems).toEqual([])
  })
})
