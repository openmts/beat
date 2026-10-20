import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { installChunkRecovery } from '@/lib/chunk-recovery'
import { initialLocale } from '@/context/locales'
import { loadPublicLocale } from '@/context/public-locales'

installChunkRecovery()
// 版本巡检不参与首屏渲染，按需加载以免占用公共外壳预算。
if (import.meta.env.PROD) {
  void import("@/lib/version-watch")
    .then((watch) => { watch.installVersionWatch() })
    .catch(() => undefined)
}

// 语言分片先于首屏渲染加载完成，避免先出现英文再跳转目标语言。
const initial = initialLocale(localStorage, navigator)
void Promise.all([loadPublicLocale(initial)]).finally(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
