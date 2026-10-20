// 分片请求在网络上挂起时不会有任何失败事件，Suspense 回退会一直停在加载态，
// 用户只能手动刷新。加载态超过宽限期仍未结束就自动整页恢复一次，避免永久卡住。
// 该模块按需加载，不进入公共外壳的分片图。

import { recoverFromChunkFailure, type ChunkRecoveryDeps } from "@/lib/chunk-recovery"

/** 加载态持续超过该时长仍未结束，视为分片已挂起。 */
export const STALLED_CHUNK_GRACE_MS = 10_000

export type StalledChunkDeps = ChunkRecoveryDeps & {
  graceMs?: number
  setTimer?: (handler: () => void, timeout: number) => number
  clearTimer?: (timer: number) => void
}

/** 注册挂起看门狗，返回取消函数；成功加载的页面会在卸载时取消。 */
export function watchStalledChunk(deps: StalledChunkDeps = {}): () => void {
  const setTimer = deps.setTimer ?? ((handler, timeout) => window.setTimeout(handler, timeout))
  const clearTimer = deps.clearTimer ?? (timer => window.clearTimeout(timer))
  const timer = setTimer(() => { recoverFromChunkFailure(deps) }, deps.graceMs ?? STALLED_CHUNK_GRACE_MS)
  return () => clearTimer(timer)
}
