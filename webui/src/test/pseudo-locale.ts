import { type Catalog, type Locale } from "@/context/locales"

/**
 * 伪语言使用的载体语言代码：`pseudo` 不属于目录契约里的任何语言，既不在 shippedLocales
 * 也不在 draftLocales，因此这份测试目录永远不会被打进产品包，也不会和真实翻译互相覆盖。
 * 该断言由 `Locale` 的封闭联合类型之外的取值保证，故这里显式收窄一次。
 */
export const pseudoCarrierLocale: Locale = "pseudo" as Locale

/** 词内膨胀比例：40% 接近德语/芬兰语的实测膨胀，用来暴露截断与换行问题。 */
const wordExpansionRatio = 0.4
/** 伪语言外层界定符：肉眼即可分辨“这条文案来自伪语言”。 */
const openMarker = "⟦"
const closeMarker = "⟧"

/**
 * pseudoValue 生成一条伪语言文案：占位符原样保留，ASCII 词按比例加长，整体加界定符。
 * 数字、标点与 CJK 原文保持不变，便于人工比对渲染结果。
 */
export function pseudoValue(value: string): string {
  const parts = value.split(/(\{\w+\})/g)
  return `${openMarker}${parts.map((part) => (part.startsWith("{") && part.endsWith("}") ? part : expandWords(part))).join("")}${closeMarker}`
}

function expandWords(value: string): string {
  return value.replace(/[A-Za-z]+/g, (word) => word + "x".repeat(Math.max(1, Math.ceil(word.length * wordExpansionRatio))))
}

/** pseudoCatalog 把整份英文目录转成伪语言目录，键集与英文完全一致。 */
export function pseudoCatalog(reference: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(reference).map(([key, value]) => [key, pseudoValue(value)]))
}

/** pseudoCatalogs 供浏览器演练装载：只带伪语言载体语言，避免误用真实翻译。 */
export function pseudoCatalogs(catalog: Record<string, string>): Catalog[] {
  return [{ [pseudoCarrierLocale]: pseudoCatalog(catalog) }]
}

export const pseudoLocaleMarker = `${openMarker}${closeMarker}`
