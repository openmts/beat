import "@/context/admin-catalogs"
import "@/context/metric-workspace-catalogs"
import { registerLocaleCatalogs } from "@/context/locale"
import { zhCNAdminCatalog } from "@/context/admin-locale-shards/zh-cn"
import { zhTWAdminCatalog } from "@/context/admin-locale-shards/zh-tw"
import { jaAdminCatalog } from "@/context/admin-locale-shards/ja"
import { idAdminCatalog } from "@/context/admin-locale-shards/id"

// 生产环境按语言懒加载管理端目录；测试一次性登记全部语言，避免每个用例都要等异步分片。
registerLocaleCatalogs([zhCNAdminCatalog, zhTWAdminCatalog, jaAdminCatalog, idAdminCatalog])
import "@testing-library/jest-dom/vitest"
import { cleanup, configure } from "@testing-library/react"
import { afterEach } from "vitest"

// 路由页面按分片懒加载，并行 worker 下动态 import 常超过 async 查询 1 秒的默认上限。
configure({ asyncUtilTimeout: 5000 })

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  }),
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  sessionStorage.clear()
})
