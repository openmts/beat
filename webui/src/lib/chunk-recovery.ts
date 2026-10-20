// 发布替换静态资源后，旧标签页引用的路由分片会 404，路由就会停在原地。
// 首次分片加载失败时自动整页加载一次；窗口内重复失败交回错误边界，避免刷新循环。

const GUARD_KEY = "beat:chunk-recovery-at"
const GUARD_WINDOW_MS = 30_000

export type ChunkStorage = Pick<Storage, "getItem" | "setItem">
export type ChunkRecoveryDeps = { storage?: ChunkStorage | null; reload?: () => void; now?: () => number }

let lastAttemptAt = 0

function browserStorage(): ChunkStorage | null {
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

function readStamp(storage: ChunkStorage | null): number {
  if (!storage) return 0
  const stored = Number.parseInt(storage.getItem(GUARD_KEY) ?? "", 10)
  return Number.isFinite(stored) ? stored : 0
}

function rememberAttempt(storage: ChunkStorage | null, stamp: number): void {
  lastAttemptAt = stamp
  try {
    storage?.setItem(GUARD_KEY, String(stamp))
  } catch {
    // 存储不可写时由内存时间戳兜底
  }
}

export function recoverFromChunkFailure(deps: ChunkRecoveryDeps = {}): boolean {
  const clock = deps.now ?? Date.now
  const storage = "storage" in deps ? deps.storage ?? null : browserStorage()
  const stamp = clock()
  const previous = Math.max(lastAttemptAt, readStamp(storage))
  if (previous > 0 && stamp - previous < GUARD_WINDOW_MS) return false
  rememberAttempt(storage, stamp)
  const reload = deps.reload ?? (() => window.location.reload())
  reload()
  return true
}

export function installChunkRecovery(target: EventTarget = window, deps: ChunkRecoveryDeps = {}): void {
  target.addEventListener("vite:preloadError", () => { recoverFromChunkFailure(deps) })
}
