// 发布替换入口分片后，长期未刷新的标签页仍运行旧代码：点击链接时路由停在旧页面，
// 必须手动刷新才恢复。这里比对服务端当前下发的入口分片，发现新构建就自动整页加载
// 一次；同一入口只触发一次，避免刷新循环。用户正在编辑时不打断，下一轮再切。

import type { ChunkStorage } from "@/lib/chunk-recovery"

const KEY = "beat:version-reloaded"
const INTERVAL_MS = 60_000
const editableSelector = "input, textarea, select, [contenteditable='true']"

export type VersionResponse = { ok: boolean; text: () => Promise<string> }
export type VersionWatchDeps = {
  document?: Document
  storage?: ChunkStorage | null
  fetchImpl?: (url: string) => Promise<VersionResponse>
  reload?: () => void
  intervalMs?: number
}

let last = ""

/** 运行中的入口分片来自当前文档的 module script。 */
export function runningEntry(scope: Document): string | null {
  return scope.querySelector("script[type=module][src]")?.getAttribute("src")?.trim() || null
}

/** 服务端下发的入口分片；构建工具把 `type` 放在 `src` 前，这里接受两种顺序。 */
export function servedEntry(html: string): string | null {
  const found = /<script\b[^>]*\btype="module"[^>]*\bsrc="([^"]+)"/i.exec(html)
    ?? /<script\b[^>]*\bsrc="([^"]+)"[^>]*\btype="module"/i.exec(html)
  return found?.[1]?.trim() || null
}

/** 该入口分片是否首次出现；持久记录保证刷新后不会再次触发。 */
function claim(target: ChunkStorage | null, entry: string): boolean {
  let known = last
  try {
    known = target?.getItem(KEY) || known
  } catch {
    // 读取失败时由内存记录兜底
  }
  if (known === entry) return false
  last = entry
  try {
    target?.setItem(KEY, entry)
  } catch {
    // 写入失败时由内存记录兜底
  }
  return true
}

function store(): ChunkStorage | null {
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

/** 只有标签页可见、没有弹窗、用户也没在编辑控件里时才允许切换构建，避免打断输入。 */
function switchable(scope: Document): boolean {
  if (scope.visibilityState !== "visible") return false
  if (scope.querySelector('[role="dialog"], [role="alertdialog"]')) return false
  const active = scope.activeElement
  if (!active || active === scope.body) return true
  return !(active instanceof Element && active.matches(editableSelector))
}

/** 单次比对；发现新构建并完成整页加载时返回 true，其余情况保持当前页面。 */
export async function checkForNewBuild(deps: VersionWatchDeps = {}): Promise<boolean> {
  const scope = deps.document ?? document
  if (!switchable(scope)) return false
  const running = runningEntry(scope)
  if (!running) return false
  try {
    const response = await (deps.fetchImpl ?? ((url: string) => window.fetch(url, { cache: "no-store" })))("/index.html")
    if (!response.ok) return false
    const served = servedEntry(await response.text())
    if (!served || served === running || served.endsWith(running)) return false
    if (!claim("storage" in deps ? deps.storage ?? null : store(), served)) return false
    const reload = deps.reload ?? (() => window.location.reload())
    reload()
    return true
  } catch {
    return false
  }
}

/** 页面可见、从前进后退缓存恢复时立即比对，之后在可见性变化与固定间隔继续比对。 */
export function installVersionWatch(deps: VersionWatchDeps = {}): () => void {
  const check = () => { void checkForNewBuild(deps) }
  const scope = deps.document ?? document
  check()
  scope.addEventListener("visibilitychange", check)
  // 从 bfcache 恢复的文档不会重新执行入口分片，手机/桌面浏览器也可能不派发可见性变化，
  // 只能用 pageshow 作为恢复信号，否则长期驻留的旧页面会一直停留在旧构建。
  window.addEventListener("pageshow", check)
  const timer = window.setInterval(check, deps.intervalMs ?? INTERVAL_MS)
  return () => {
    scope.removeEventListener("visibilitychange", check)
    window.removeEventListener("pageshow", check)
    window.clearInterval(timer)
  }
}
