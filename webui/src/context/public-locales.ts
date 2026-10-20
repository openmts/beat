import type { Catalog, Locale } from "@/context/locales"

// 公共外壳只把英文目录内联进入口分片；其余语言放在按语言拆分的分片里，只在需要时加载。
// 这样做保证「公共图 + 该访客的语言分片」始终不超过 250 KiB gzip，语言数量增加不再挤压首屏预算。
const loaders: Partial<Record<Locale, () => Promise<Catalog>>> = {
  "zh-CN": () => import("@/context/public-locales/zh-cn").then((module) => module.zhCNPublicCatalog),
  "zh-TW": () => import("@/context/public-locales/zh-tw").then((module) => module.zhTWPublicCatalog),
  "ja": () => import("@/context/public-locales/ja").then((module) => module.jaPublicCatalog),
  "id": () => import("@/context/public-locales/id").then((module) => module.idPublicCatalog),
}

const loaded: Catalog[] = []

// loadedPublicCatalogs 交给 translateFrom 合并；英文目录由调用方静态传入。
export function loadedPublicCatalogs(): Catalog[] {
  return loaded
}

// loadPublicLocale 幂等：同一种语言重复调用只加载一次；返回值表示目录是否发生了变化，
// 调用方据此决定是否需要额外渲染一次（英文没有分片，因此不会产生多余渲染）。
export async function loadPublicLocale(locale: Locale): Promise<boolean> {
  const load = loaders[locale]
  if (!load || loaded.some((catalog) => catalog[locale] !== undefined)) return false
  try {
    const catalog = await load()
    if (catalog[locale] === undefined || loaded.includes(catalog)) return false
    loaded.push(catalog)
    return true
  } catch {
    // 语言分片加载失败只影响该语言的文案，回退到英文不影响功能。
    return false
  }
}

// resetPublicLocaleCatalogs 只供测试隔离使用。
export function resetPublicLocaleCatalogs(): void {
  loaded.length = 0
}
