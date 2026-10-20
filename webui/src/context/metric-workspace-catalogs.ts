import { registerLocaleCatalogs } from "@/context/locale"
import { metricWorkspaceTranslations } from "@/context/metric-workspace-translations"

// 指标工作区目录是最大的一份文案（五语言约 157 kB 源码），只有工作区与探索页会渲染它。
// 跟随这两个路由分片注册后，后台其它页面不再下载这份目录，单分片预算也不被它挤占；
// 与 metric-explore 目录在路由内注册的做法保持一致。
registerLocaleCatalogs([metricWorkspaceTranslations])
