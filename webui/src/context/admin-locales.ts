import { registerLocaleCatalogs } from "@/context/locale"
import type { Catalog, Locale } from "@/context/locales"

// 管理端目录分片：按语言懒加载，管理端入口分片不再随语言数量增长。
// 英文目录仍随入口分片内联，作为兜底语言；其余语言在渲染管理端之前加载完成。
const loaders: Partial<Record<Locale, () => Promise<Catalog>>> = {
  "zh-CN": () => import("@/context/admin-locale-shards/zh-cn").then((module) => module.zhCNAdminCatalog),
  "zh-TW": () => import("@/context/admin-locale-shards/zh-tw").then((module) => module.zhTWAdminCatalog),
  ja: () => import("@/context/admin-locale-shards/ja").then((module) => module.jaAdminCatalog),
  id: () => import("@/context/admin-locale-shards/id").then((module) => module.idAdminCatalog),
}

const loaded: Catalog[] = []

// loadedAdminCatalogs 交给 translateFrom 合并；英文目录由调用方静态传入。
export function loadedAdminCatalogs(): Catalog[] {
  return loaded
}

// hasAdminLocale 判断某语言的目录是否已经可用，用于避免英文回退闪烁。
export function hasAdminLocale(locale: Locale): boolean {
  return loaded.some((catalog) => catalog[locale] !== undefined)
}

// adminLocalePending 判断该语言是否仍在等待分片。英文目录随入口分片内联，永远不需要等待，
// 因此英文环境的管理端可以在第一帧同步渲染。
export function adminLocalePending(locale: Locale): boolean {
  return loaders[locale] !== undefined && !hasAdminLocale(locale)
}

// loadAdminLocale 幂等：同一种语言重复调用只加载一次；返回值表示目录是否发生变化。
export async function loadAdminLocale(locale: Locale): Promise<boolean> {
  const load = loaders[locale]
  if (!load || loaded.some((catalog) => catalog[locale] !== undefined)) return false
  try {
    const catalog = await load()
    if (catalog[locale] === undefined || loaded.includes(catalog)) return false
    loaded.push(catalog)
    // 通过既有注册通道并通知 Provider 重渲染，公共外壳无需引用管理端目录。
    registerLocaleCatalogs([catalog])
    return true
  } catch {
    // 语言分片加载失败只影响该语言的文案，回退到英文不影响功能。
    return false
  }
}

// resetAdminLocaleCatalogs 只供测试隔离使用。
export function resetAdminLocaleCatalogs(): void {
  loaded.length = 0
}
