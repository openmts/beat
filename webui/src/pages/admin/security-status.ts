import type { SecurityEvent } from "@/types/security"

type BadgeVariant = "default" | "secondary" | "destructive" | "outline"

/** 严重级别与投递状态共用固定映射，避免各处自行判断颜色。 */
export function securitySeverityVariant(severity: string): BadgeVariant {
  if (severity === "high") return "destructive"
  if (severity === "medium") return "default"
  return "outline"
}

export function securityStateVariant(state: string): BadgeVariant {
  if (state === "failed" || state === "unavailable") return "destructive"
  if (state === "delivered" || state === "routed") return "secondary"
  return "outline"
}

/** 事件是否存在未投递或失败的通知；用于列表与详情的高亮提示。 */
export function securityDeliveryAttention(event: SecurityEvent): boolean {
  return event.delivery_failures > 0 || event.delivery_state === "failed"
}
