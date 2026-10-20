import { APIError } from "@/lib/api-error"

// 部署替换、反向代理抖动或限流会让工作区首屏整体失败；只重试一次会让页面一直停在
// 错误态，用户必须手动刷新才能恢复。这里按退避重试到固定上限，覆盖服务端重启窗口。
const backoffMs = [400, 1_000, 2_500, 5_000, 8_000, 12_000]
const maxDelayMs = 30_000

/** 可重试的状态码：网络中断、超时、限流与服务端错误；其余 4xx 视为确定性失败。 */
function retryable(status: number): boolean {
  return status === 0 || status === 408 || status === 425 || status === 429 || status >= 500
}

/** 返回下一次重试的等待毫秒；返回 null 表示错误不可重试或已达到重试上限。 */
export function workspaceRetryDelay(error: unknown, attempt: number): number | null {
  if (attempt >= backoffMs.length) return null
  const status = error instanceof APIError ? error.status : 0
  if (!retryable(status)) return null
  const retryAfter = error instanceof APIError ? error.retryAfter : null
  if (retryAfter !== null) return Math.min(retryAfter * 1_000, maxDelayMs)
  return backoffMs[attempt]
}

/** 首屏退避预算用尽后的慢速恢复节奏：升级窗口、重启或限流冷却可能长于 30 s 退避预算。 */
export const workspaceRecoveryIntervalMs = 20_000
const recoveryAttempts = 15
/** 慢速预算之后的驻留节奏：长期开着的标签页仍会自愈，不必手动刷新。 */
export const workspaceIdleRecoveryIntervalMs = 60_000

/** 返回慢速恢复的下一次等待毫秒；返回 null 表示确定性失败，其余情况一直保持低频重试。 */
export function workspaceRecoveryDelay(error: unknown, recoveryAttempt: number): number | null {
  const status = error instanceof APIError ? error.status : 0
  if (!retryable(status)) return null
  const retryAfter = error instanceof APIError ? error.retryAfter : null
  if (retryAfter !== null) return Math.max(workspaceRecoveryIntervalMs, Math.min(retryAfter * 1_000, maxDelayMs))
  // 升级/回滚窗口可能远长于 5 分钟慢速预算；预算用尽后降到每分钟一次，页面自己恢复而不是
  // 永久停在「必须手动刷新」的错误态。
  return recoveryAttempt < recoveryAttempts ? workspaceRecoveryIntervalMs : workspaceIdleRecoveryIntervalMs
}
