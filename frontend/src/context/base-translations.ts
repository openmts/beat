import { baseTranslationsEn } from "@/context/base-translations-en"
import { baseTranslationsZhCN } from "@/context/base-translations-zh-cn"

export type Locale = "en" | "zh-CN"

export const baseTranslations: Record<Locale, Record<string, string>> = {
  en: baseTranslationsEn,
  "zh-CN": baseTranslationsZhCN,
}
