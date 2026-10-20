import type { EgressException, NotificationSettings } from "@/types/notification"

export interface ExceptionDraft { mode: "host" | "cidr"; target: string; port: string; purpose: string; expires: string }
export interface NotificationSettingsForm {
  revision: number
  successDays: string
  failedDays: string
  workers: string
  exceptions: ExceptionDraft[]
}

export function notificationSettingsForm(value: NotificationSettings): NotificationSettingsForm {
  return { revision: value.revision, successDays: String(value.success_retention_days), failedDays: String(value.dead_retention_days), workers: String(value.maximum_workers),
    exceptions: value.egress_policy.exceptions.map(entry => ({ mode: entry.cidr ? "cidr" : "host", target: entry.cidr || entry.host || "", port: String(entry.port), purpose: entry.purpose, expires: localDateInput(entry.expires_at) })) }
}

export function newNotificationException(): ExceptionDraft {
  return { mode: "host", target: "", port: "443", purpose: "", expires: localDateInput(new Date(Date.now() + 86_400_000).toISOString()) }
}

export function notificationSettingsPayload(form: NotificationSettingsForm): NotificationSettings {
  return { revision: form.revision, success_retention_days: Number(form.successDays), dead_retention_days: Number(form.failedDays), maximum_workers: Number(form.workers),
    egress_policy: { revision: form.revision, exceptions: form.exceptions.map(exceptionPayload) } }
}

function exceptionPayload(draft: ExceptionDraft): EgressException {
  const expires = new Date(draft.expires)
  if (!Number.isFinite(expires.getTime())) throw new Error("notification.invalid_exception")
  return { [draft.mode]: draft.target.trim(), port: Number(draft.port), purpose: draft.purpose.trim(), expires_at: expires.toISOString() }
}

export function notificationSettingsError(form: NotificationSettingsForm): string | null {
  const numberFields = [[form.successDays, 7, 365], [form.failedDays, 30, 730], [form.workers, 1, 32]] as const
  if (numberFields.some(([raw, min, max]) => !validInteger(raw, min, max))) return "notification.settings_limits"
  if (form.exceptions.length > 64 || form.exceptions.some(invalidException)) return "notification.invalid_exception"
  return null
}

function invalidException(entry: ExceptionDraft) {
  return !entry.target.trim() || (!entry.purpose.trim() || entry.purpose.length > 200) || !validInteger(entry.port, 1, 65_535)
    || !Number.isFinite(Date.parse(entry.expires)) || Date.parse(entry.expires) <= Date.now() || Date.parse(entry.expires) > Date.now() + 90 * 86_400_000
}

function validInteger(raw: string, min: number, max: number) { const value = Number(raw); return Number.isInteger(value) && value >= min && value <= max }

function localDateInput(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ""
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}
