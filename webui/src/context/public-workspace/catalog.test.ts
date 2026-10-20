import { describe, expect, it } from "vitest"
import { metricWorkspaceTranslations } from "@/context/metric-workspace-translations"
import { zhCNAdminCatalog } from "@/context/admin-locale-shards/zh-cn"
import { zhTWAdminCatalog } from "@/context/admin-locale-shards/zh-tw"
import { jaAdminCatalog } from "@/context/admin-locale-shards/ja"
import { idAdminCatalog } from "@/context/admin-locale-shards/id"
import type { Locale } from "@/context/locales"
import { publicWorkspaceEn } from "./en"
import { publicWorkspaceZhCN } from "./zh-cn"
import { publicWorkspaceZhTW } from "./zh-tw"
import { publicWorkspaceJa } from "./ja"
import { publicWorkspaceId } from "./id"

// 公共外壳只下发这份子集；一旦与管理端工作区目录脱钩，公共节点页就会重新显示原始键名。
const publicCatalogs: { locale: Locale; entries: Record<string, string> }[] = [
  { locale: "en", entries: publicWorkspaceEn.en ?? {} },
  { locale: "zh-CN", entries: publicWorkspaceZhCN["zh-CN"] ?? {} },
  { locale: "zh-TW", entries: publicWorkspaceZhTW["zh-TW"] ?? {} },
  { locale: "ja", entries: publicWorkspaceJa.ja ?? {} },
  { locale: "id", entries: publicWorkspaceId.id ?? {} },
]

describe("public metric workspace catalog", () => {
  it("keeps the same keys in every language", () => {
    const reference = Object.keys(publicCatalogs[0]!.entries).sort()
    expect(reference.length).toBeGreaterThan(80)
    for (const catalog of publicCatalogs) expect(Object.keys(catalog.entries).sort()).toEqual(reference)
  })

  // 管理端工作区目录按语言拆分：英文在入口目录里，其余语言在语言分片里。
  const workspaceSource = (locale: Locale): Record<string, string> => {
    if (locale === "en") return metricWorkspaceTranslations.en ?? {}
    if (locale === "zh-CN") return zhCNAdminCatalog["zh-CN"] ?? {}
    if (locale === "zh-TW") return zhTWAdminCatalog["zh-TW"] ?? {}
    if (locale === "ja") return jaAdminCatalog.ja ?? {}
    return idAdminCatalog.id ?? {}
  }

  it("matches the metric workspace catalog for every key", () => {
    const problems: string[] = []
    for (const catalog of publicCatalogs) {
      const source = workspaceSource(catalog.locale)
      for (const [key, value] of Object.entries(catalog.entries)) {
        if (source[key] !== value) problems.push(`${catalog.locale} ${key}`)
      }
    }
    expect(problems).toEqual([])
  })

  it("covers the read-only controls rendered on the public node page", () => {
    const present = new Set(Object.keys(publicWorkspaceEn.en ?? {}))
    const required = ["workspace.time_range", "workspace.refresh_interval", "workspace.seconds", "workspace.off",
      "workspace.last1h", "workspace.custom_range", "workspace.from", "workspace.to", "workspace.no_data",
      "workspace.last_success", "workspace.updating", "workspace.retry", "workspace.legend.series", "workspace.drag_zoom",
      "workspace.heatmap_samples", "workspace.histogram_samples", "workspace.state_timeline_states"]
    expect(required.filter((key) => !present.has(key))).toEqual([])
  })
})
