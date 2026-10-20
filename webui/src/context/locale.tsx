import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"
import { acceleratorTranslations } from "@/context/accelerator-translations"
import { baseTranslations } from "@/context/base-translations"
import { fleetTranslations } from "@/context/fleet-translations"
import {
  initialLocale,
  translateFrom,
  writeStoredLocale,
  type Catalog,
  type Locale,
  type NavigatorLanguages,
  type Translate,
} from "@/context/locales"
import { monitorTranslations } from "@/context/monitor-translations"
import { loadPublicLocale, loadedPublicCatalogs } from "@/context/public-locales"
import { trafficReportTranslations } from "@/context/traffic-report-translations"
import { themeTranslations } from "@/context/theme-translations"
import { telemetryTranslations } from "@/context/telemetry-translations"

// publicCatalogs 是公共外壳会渲染的目录，只内联英文；其余语言由 public-locales 按需加载，
// 管理端目录由 admin-catalogs 注册，三者都不会把多余语言压进 250 KiB 的公共外壳预算。
const publicCatalogs: Catalog[] = [
  baseTranslations,
  monitorTranslations,
  trafficReportTranslations,
  themeTranslations,
  telemetryTranslations,
  fleetTranslations,
  acceleratorTranslations,
]

const registeredCatalogs: Catalog[] = []
const catalogListeners = new Set<() => void>()

// registerLocaleCatalogs 由管理端分片在 import 时调用，重复注册会被忽略。
// 语言分片可能在首屏之后才注册，注册时必须通知已挂载的 Provider 重新渲染，否则会短暂回退英文。
export function registerLocaleCatalogs(catalogs: Catalog[]): void {
  let changed = false
  for (const catalog of catalogs) {
    if (!registeredCatalogs.includes(catalog)) {
      registeredCatalogs.push(catalog)
      changed = true
    }
  }
  if (!changed) return
  for (const listener of catalogListeners) listener()
}

// subscribeLocaleCatalogs 只给 LocaleProvider 使用，用于在目录迟到时重渲染。
export function subscribeLocaleCatalogs(listener: () => void): () => void {
  catalogListeners.add(listener)
  return () => { catalogListeners.delete(listener) }
}

// resetLocaleCatalogs 只供测试隔离使用。
export function resetLocaleCatalogs(): void {
  registeredCatalogs.length = 0
}

interface LocaleContextValue {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: Translate
}

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined)

// currentStorage 只在浏览器可用时返回 localStorage；测试与服务端渲染都允许缺省。
function currentStorage(): Storage | undefined {
  return typeof localStorage === "undefined" ? undefined : localStorage
}

function browserLanguages(): NavigatorLanguages | undefined {
  return typeof navigator === "undefined" ? undefined : navigator
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(() => initialLocale(currentStorage(), browserLanguages()))
  const [publicLocaleCatalogs, setPublicLocaleCatalogs] = useState<Catalog[]>(() => [...loadedPublicCatalogs()])
  // catalogVersion 只用于在迟到的语言分片注册后触发一次重渲染。
  const [catalogVersion, setCatalogVersion] = useState(0)

  const t = useCallback(
    (key: string, params?: Record<string, string | number>): string =>
      translateFrom([...publicCatalogs, ...publicLocaleCatalogs, ...registeredCatalogs], locale, key, params),
    [locale, publicLocaleCatalogs, catalogVersion],
  )

  useEffect(() => subscribeLocaleCatalogs(() => setCatalogVersion((version) => version + 1)), [])

  // 首屏由入口先加载再渲染；这里兜底处理直接挂载 Provider 的场景（测试、嵌入式用法）。
  useEffect(() => {
    let active = true
    void loadPublicLocale(locale).then((changed) => {
      if (active && changed) setPublicLocaleCatalogs([...loadedPublicCatalogs()])
    })
    return () => { active = false }
  }, [locale])

  // 语言分片先加载再切换，避免先渲染英文再跳到目标语言；分片命中缓存后切换是瞬时的。
  const setLocaleAndPersist = useCallback((newLocale: Locale) => {
    void loadPublicLocale(newLocale).finally(() => {
      writeStoredLocale(currentStorage(), newLocale)
      setPublicLocaleCatalogs([...loadedPublicCatalogs()])
      setLocale(newLocale)
    })
  }, [])

  return (
    <LocaleContext.Provider value={{ locale, setLocale: setLocaleAndPersist, t }}>
      {children}
    </LocaleContext.Provider>
  )
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error("useLocale must be used within LocaleProvider")
  return ctx
}

// useOptionalLocale 供 ui/ 下的共享组件使用：没有 LocaleProvider 时返回 null，
// 组件据此退化成「无文案」而不是抛错，单测可以脱离 Provider 独立渲染。
export function useOptionalLocale(): LocaleContextValue | null {
  return useContext(LocaleContext) ?? null
}
