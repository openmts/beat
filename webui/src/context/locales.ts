// 语言与目录契约：完整目录清单是唯一真源，语言菜单与浏览器语言识别都只使用
// shippedLocales；新增语言必须先补全目录再登记，未完成的语言不会出现在界面上。

export type Locale = "en" | "zh-CN" | "zh-TW" | "ja" | "id"

/** Translate 是文案目录的查询函数：缺键时按语言链回退，最后原样返回键名。 */
export type Translate = (key: string, params?: Record<string, string | number>) => string

export interface LocaleOption {
  code: Locale
  /** 以该语言自身书写的名称，菜单直接展示，避免使用者看不懂当前界面语言。 */
  nativeName: string
  /** 菜单里的短码，与原生名称一起出现，消除歧义。 */
  label: string
}

/** 已发布语言：必须拥有全部公共与管理端目录的完整键集。 */
export const shippedLocales: readonly Locale[] = ["en", "zh-CN", "zh-TW", "ja", "id"]

/**
 * 正在翻译、尚未发布的语言：目录允许缺键，但不会出现在语言菜单、浏览器识别或语言回退里，
 * 因此不完整的翻译不会影响任何使用者。补齐后必须移入 shippedLocales，门禁会检查这一步。
 */
export const draftLocales: readonly Locale[] = []

export const localeOptions: readonly LocaleOption[] = [
  { code: "en", nativeName: "English", label: "EN" },
  { code: "zh-CN", nativeName: "简体中文", label: "简" },
  { code: "zh-TW", nativeName: "繁體中文", label: "繁" },
  { code: "ja", nativeName: "日本語", label: "JA" },
  { code: "id", nativeName: "Bahasa Indonesia", label: "ID" },
]

export type Catalog = Partial<Record<Locale, Record<string, string>>>

const storageKey = "locale"

function normalize(tag: string): string {
  return tag.trim().toLowerCase().replaceAll("_", "-")
}

function exactLocale(tag: string, available: readonly Locale[]): Locale | undefined {
  return available.find((code) => normalize(code) === tag)
}

/** 语言族候选：同语言优先同书写系统，再退到另一种书写系统。 */
function familyCandidates(tag: string): Locale[] {
  const [language] = tag.split("-")
  if (language === "zh") return /hant|-(tw|hk|mo)\b/.test(tag) ? ["zh-TW", "zh-CN"] : ["zh-CN", "zh-TW"]
  if (language === "ja") return ["ja"]
  if (language === "id" || language === "in") return ["id"]
  if (language === "en") return ["en"]
  return []
}

/** 精确匹配优先，其次语言族，最后英文；不支持的候选一律忽略。 */
export function resolveLocale(candidates: readonly string[], available: readonly Locale[] = shippedLocales): Locale {
  const tags = candidates.map(normalize).filter((tag) => tag.length > 0)
  for (const tag of tags) {
    const exact = exactLocale(tag, available)
    if (exact) return exact
  }
  const allowed = new Set(available)
  for (const tag of tags) {
    const family = familyCandidates(tag).find((candidate) => allowed.has(candidate))
    if (family) return family
  }
  return "en"
}

/** 已发布语言之间的回退目标：繁体中文缺键时退回简体，而不是显示英文。 */
export function fallbackLocales(locale: Locale): readonly Locale[] {
  return locale === "zh-TW" ? ["zh-CN", "en"] : ["en"]
}

export interface LocaleStorage {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

/** 存储被拒绝（隐私模式、禁用 Cookie）时返回 undefined，渲染流程不受影响。 */
export function readStoredLocale(storage: LocaleStorage | undefined): Locale | undefined {
  if (!storage) return undefined
  try {
    const stored = storage.getItem(storageKey)
    return stored ? exactLocale(normalize(stored), shippedLocales) : undefined
  } catch {
    return undefined
  }
}

export function writeStoredLocale(storage: LocaleStorage | undefined, locale: Locale): void {
  if (!storage) return
  try {
    storage.setItem(storageKey, locale)
  } catch {
    // 存储被拒绝只影响下次访问的默认值，当前会话仍然切换语言。
  }
}

export interface NavigatorLanguages {
  language?: string
  languages?: readonly string[]
}

export function browserLocaleCandidates(navigatorLike: NavigatorLanguages | undefined): string[] {
  if (!navigatorLike) return []
  const list = navigatorLike.languages && navigatorLike.languages.length > 0
    ? [...navigatorLike.languages]
    : [navigatorLike.language ?? ""]
  return list.filter((tag) => tag.length > 0)
}

/** 语言在目录中出现的键集合；未登记的目录返回空集。 */
export function catalogKeys(catalog: Catalog, locale: Locale): string[] {
  return Object.keys(catalog[locale] ?? {})
}

/** 相对英文目录缺失的键；顺序稳定，便于门禁输出可复现的差异。 */
export function catalogGaps(catalogs: readonly Catalog[], locale: Locale): string[] {
  const reference = new Set<string>()
  for (const catalog of catalogs) for (const key of catalogKeys(catalog, "en")) reference.add(key)
  const present = new Set<string>()
  for (const catalog of catalogs) for (const key of catalogKeys(catalog, locale)) present.add(key)
  return [...reference].filter((key) => !present.has(key))
}

/** 初始化顺序：已保存的选择、浏览器精确语言、语言族、英文。 */
export function initialLocale(storage: LocaleStorage | undefined, navigatorLike: NavigatorLanguages | undefined): Locale {
  const stored = readStoredLocale(storage)
  if (stored) return stored
  return resolveLocale(browserLocaleCandidates(navigatorLike))
}

/**
 * 目录查找：当前语言 → 语言族回退 → 英文。占位符按 `{name}` 替换；
 * 找不到键时返回键本身，方便门禁发现漏翻。
 */
export function translateFrom(
  catalogs: readonly Catalog[],
  locale: Locale,
  key: string,
  params?: Record<string, string | number>,
): string {
  const candidates = [locale, ...fallbackLocales(locale)]
  const translation = candidates
    .flatMap((candidate) => catalogs.map((catalog) => catalog[candidate]?.[key]))
    .find((value) => value !== undefined)
  if (translation === undefined) return key
  if (!params) return translation
  return translation.replace(/\{(\w+)\}/g, (placeholder, name) =>
    String(params[name] ?? placeholder),
  )
}

/** prefixedTranslator 供薄文案 hook 使用：目录缺少该键时原样返回调用方字符串，
 *  保持迁移前 `key in table ? table[key] : key` 的透传语义（错误信息、动态值不受影响）。 */
export function prefixedTranslator(
  t: (key: string, params?: Record<string, string | number>) => string,
  prefix: string,
): (key: string, params?: Record<string, string | number>) => string {
  return (key, params) => {
    const prefixed = `${prefix}.${key}`
    const value = t(prefixed, params)
    return value === prefixed ? key : value
  }
}
