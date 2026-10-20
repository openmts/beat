import { describe, expect, it } from "vitest"
import { adminShellTranslations } from "./admin-shell-translations"
import { metricWorkspaceTranslations } from "./metric-workspace-translations"
import { zhCNAdminCatalog } from "./admin-locale-shards/zh-cn"
import { zhTWAdminCatalog } from "./admin-locale-shards/zh-tw"
import { jaAdminCatalog } from "./admin-locale-shards/ja"
import { idAdminCatalog } from "./admin-locale-shards/id"
import { shippedLocales, type Locale } from "./locales"

describe("admin shell catalog", () => {
  // 外壳分片移出工作区目录后，侧边栏仍必须能解析这些键；这里同时守住两份文案的取值一致。
  // 工作区目录同样按语言拆分：英文在入口目录里，其余语言在对应的语言分片里。
  const workspaceTitle = (locale: Locale): string | undefined => {
    if (locale === "en") return metricWorkspaceTranslations.en?.["workspace.title"]
    const source: Record<string, string> = locale === "zh-CN" ? (zhCNAdminCatalog["zh-CN"] ?? {})
      : locale === "zh-TW" ? (zhTWAdminCatalog["zh-TW"] ?? {})
      : locale === "ja" ? (jaAdminCatalog.ja ?? {}) : (idAdminCatalog.id ?? {})
    return source["workspace.title"]
  }

  it("keeps the shell keys in sync with the metric workspace catalog", () => {
    for (const locale of shippedLocales) {
      expect(adminShellTranslations[locale]?.["workspace.title"]).toBe(workspaceTitle(locale))
    }
  })

  it("covers every shipped locale", () => {
    for (const locale of shippedLocales) expect(adminShellTranslations[locale]?.["workspace.title"]).toBeTruthy()
  })
})
