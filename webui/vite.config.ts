import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import { publicShellPlugin } from './scripts/public-shell-plugin.ts'

// 管理端英文兜底目录只被后台分片引用，却占了后台入口分片的一半以上。
// 单独成块后，后台入口分片与目录分块各自稳定落在私有分片 600 KiB 上限内，新增文案不再顶破门禁。
// 公共外壳不引用这些模块，因此它们不会进入公共图预算。
// 指标工作区目录最大，它跟随 metric-workspace 路由分片注册，不再进目录分块。
// 非英文语言在 context/admin-locale-shards/<语言>.ts 里按需加载，只有当前语言会被下载。
const adminCatalogNames = new Set([
  "agent-enrollment", "agent-rollout", "agent-setup", "application-retention", "backup", "diagnostics", "evaluation",
  "ingress", "lifecycle", "maintenance", "migration", "metric-erasure", "metric-explore",
  "notification", "notification-field", "operation", "secret", "security", "site-settings", "theme-market",
])

// 只收 `context/<feature>-translations.ts`（英文兜底目录）。
// 非英文目录改为 `context/admin-locale-shards/<语言>.ts` 按需加载的语言分片，
// 因此新增语言只增加一个分片，不再撑大任何常驻分片。
// `context/site-settings.tsx` 之类被公共外壳引用的模块不能落进来，否则整个目录分块会被算进公共预算。
function isAdminCatalog(id: string): boolean {
  const feature = /[\\/]src[\\/]context[\\/]([^\\/]+)-translations\.ts$/.exec(id)
  return feature !== null && adminCatalogNames.has(feature[1])
}

export default defineConfig({
  plugins: [react(), tailwindcss(), publicShellPlugin()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    rolldownOptions: {
      output: { advancedChunks: { groups: [{ name: "admin-catalogs", test: isAdminCatalog }] } },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/main.tsx', 'src/types/**', 'src/components/ui/**', 'src/test/**'],
      thresholds: {
        lines: 90,
      },
    },
  },
})
