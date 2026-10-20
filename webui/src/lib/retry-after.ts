import { APIError } from "@/lib/api-error"

/** 服务端 Retry-After 的等待上限：更长的等待会让页面看起来像卡住。 */
export const maximumRetryAfterMs = 300_000

/**
 * 从失败原因推导下一次轮询延迟。返回 0 表示服务端没有要求退避，调用方沿用自身间隔；
 * 返回正数表示服务端给出了 Retry-After，调用方必须至少等这么久再请求。
 */
export function retryAfterDelay(error: unknown): number {
  if (!(error instanceof APIError) || error.retryAfter === null) return 0
  return Math.max(0, Math.min(maximumRetryAfterMs, Math.round(error.retryAfter * 1000)))
}

/** 综合服务端退避要求与调用方的常规间隔，得到实际等待时间。 */
export function pollDelay(error: unknown, intervalMs: number): number {
  return Math.max(intervalMs, retryAfterDelay(error))
}
