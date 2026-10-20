import type { EnrollmentGrant, EnrollmentRequestStatus } from "@/types/enrollment"

/** 授权在界面上的四种状态：生效、停用、过期、撤销。 */
export type GrantState = "active" | "inactive" | "expired" | "revoked"

export function grantState(grant: EnrollmentGrant, now: number): GrantState {
  if (grant.revoked_at) return "revoked"
  if (!Number.isFinite(Date.parse(grant.expires_at)) || Date.parse(grant.expires_at) <= now) return "expired"
  return grant.enabled ? "active" : "inactive"
}

export function requestStatusKey(status: EnrollmentRequestStatus): string {
  return `discovery.status_${status}`
}

/** 后端只接受 RFC3339；本地输入非法时返回空字符串，让调用方阻止提交。 */
export function parseLocalInstant(value: string): string {
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? new Date(parsed).toISOString() : ""
}

/** RFC3339 转 datetime-local 控件值，保持管理员本地时区。 */
export function localInstantInput(instant: string): string {
  const parsed = Date.parse(instant)
  if (!Number.isFinite(parsed)) return ""
  const local = new Date(parsed - new Date(parsed).getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

export function defaultGrantExpiry(now = Date.now()): string {
  return localInstantInput(new Date(now + 30 * 24 * 60 * 60 * 1000).toISOString())
}

export function splitEntries(value: string): string[] {
  return value.split(",").map((entry) => entry.trim()).filter((entry) => entry !== "")
}

export function joinEntries(values: string[] | undefined): string {
  return (values ?? []).join(", ")
}

export function splitCIDRs(value: string): string[] {
  return value.split("\n").map((line) => line.trim()).filter((line) => line !== "")
}

export function joinCIDRs(values: string[] | undefined): string {
  return (values ?? []).join("\n")
}

export function platformLabel(os: string, architecture: string): string {
  const parts = [os, architecture].map((part) => part.trim()).filter((part) => part !== "")
  return parts.length === 0 ? "" : parts.join(" · ")
}
