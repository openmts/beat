import type { Catalog } from "@/context/locales"

// 管理端外壳（侧边栏与导航）渲染的文案必须随外壳分片常驻：指标工作区目录改为随工作区路由分片注册后，
// 其它后台页面读不到它。这里只保留外壳真正用到的键，取值与管理端工作区目录一致，由单测守住漂移。
export const adminShellTranslations: Catalog = {
  en: { "workspace.title": "Metric workspace" },
  "zh-CN": { "workspace.title": "指标工作区" },
  "zh-TW": { "workspace.title": "指標工作區" },
  ja: { "workspace.title": "メトリクスワークスペース" },
  id: { "workspace.title": "Ruang kerja metrik" },
}
