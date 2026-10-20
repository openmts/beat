import { baseTranslationsEn } from "@/context/base-translations-en"
import { publicWorkspaceEn } from "@/context/public-workspace/en"
import type { Catalog } from "@/context/locales"

export type { Locale } from "@/context/locales"

// 公共外壳只内联英文：其它语言的 base 文案随 public-locales 的语言分片加载。
// 公共节点页会渲染只读指标工作区，工作区文案必须随外壳内联（见 context/public-workspace）。
export const baseTranslations: Catalog = {
  en: { ...baseTranslationsEn, ...publicWorkspaceEn.en },
}
