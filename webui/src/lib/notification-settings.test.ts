import { afterEach, describe, expect, it, vi } from "vitest"
import { newNotificationException, notificationSettingsError, notificationSettingsForm, notificationSettingsPayload } from "@/lib/notification-settings"
import { notificationSettings } from "@/test/notification-fixtures"

afterEach(() => vi.useRealTimers())

describe("notification settings forms", () => {
  it("round trips named destinations and subnets without trusting stored actor attribution", () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date("2026-09-13T00:00:00Z"))
    const settings = notificationSettings()
    settings.egress_policy.exceptions = [{ host: "alerts.internal", port: 443, purpose: "Internal receiver", expires_at: "2026-09-14T08:00:00Z", created_by: "owner" },
      { cidr: "10.1.2.0/24", port: 587, purpose: "SMTP relay", expires_at: "2026-09-15T08:00:00Z" }]
    const form = notificationSettingsForm(settings)
    expect(notificationSettingsError(form)).toBeNull()
    const payload = notificationSettingsPayload(form)
    expect(payload.egress_policy.exceptions[0].created_by).toBeUndefined()
    expect(payload.egress_policy.exceptions[1]).toEqual({ ...settings.egress_policy.exceptions[1], expires_at: "2026-09-15T08:00:00.000Z" })
    expect(newNotificationException().expires).not.toBe("")
  })
  it("bounds retained history, delivery concurrency and exceptions", () => {
    const form = notificationSettingsForm(notificationSettings())
    for (const [key, value] of [["successDays", "6"], ["successDays", "366"], ["failedDays", "29"], ["failedDays", "731"], ["workers", "0"], ["workers", "33"], ["workers", "1.5"]]) {
      expect(notificationSettingsError({ ...form, [key]: value })).toBe("notification.settings_limits")
    }
    const valid = { ...newNotificationException(), target: "alerts.internal", purpose: "Internal alerts" }
    for (const update of [{ target: "" }, { purpose: "" }, { port: "0" }, { expires: "bad" }, { expires: "2000-01-01T00:00" }, { expires: "2099-01-01T00:00" }]) {
      expect(notificationSettingsError({ ...form, exceptions: [{ ...valid, ...update }] })).toBe("notification.invalid_exception")
    }
    expect(notificationSettingsError({ ...form, exceptions: Array.from({ length: 65 }, () => valid) })).toBe("notification.invalid_exception")
    expect(() => notificationSettingsPayload({ ...form, exceptions: [{ ...valid, expires: "bad" }] })).toThrow("notification.invalid_exception")
  })
  it("does not invent a valid date or target for malformed stored entries", () => {
    const settings = notificationSettings()
    settings.egress_policy.exceptions = [{ port: 443, purpose: "test", expires_at: "invalid" }]
    expect(notificationSettingsForm(settings).exceptions[0]).toMatchObject({ target: "", expires: "" })
  })
})
