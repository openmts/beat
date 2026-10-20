import { describe, expect, it } from "vitest"
import { alertChannelFormError, alertChannelFormFrom, alertChannelPayload, channelFieldIsCleared, channelFieldPresent, emptyAlertChannelForm, serializeAlertChannelConfig } from "@/pages/admin/alert-channel-config"
import { notificationChannel } from "@/test/notification-fixtures"
import { notificationChannelTypes, notificationFields } from "@/lib/notification-fields"

describe("notification channel configuration", () => {
  it("never copies saved credentials into an editable form", () => {
    const form = alertChannelFormFrom(notificationChannel({ channel_type: "telegram", config: { bot_token: "must-not-display", chat_id: "private", parse_mode: "HTML" }, secret_present: { bot_token: true, chat_id: true } }))
    expect(form.values.bot_token).toBe("")
    expect(form.values.chat_id).toBe("")
    expect(form.values.parse_mode).toBe("HTML")
    expect(alertChannelFormError(form)).toBeNull()
    expect(serializeAlertChannelConfig(form)).toEqual({ parse_mode: "HTML" })
    expect(alertChannelPayload(form)).toMatchObject({ expected_revision: 3, config: { parse_mode: "HTML" } })
  })

  it("retains blank secrets and separates explicit clearing", () => {
    const form = alertChannelFormFrom(notificationChannel({ secret_present: { endpoint: true, headers: true } }))
    expect(serializeAlertChannelConfig(form)).toEqual({})
    form.clearSecrets = ["headers"]
    expect(alertChannelPayload(form)).toMatchObject({ config: {}, clear_secrets: ["headers"] })
    form.clearSecrets.push("endpoint")
    expect(alertChannelFormError(form)).toBe("notification.required_fields")
    form.enabled = false
    expect(alertChannelFormError(form)).toBeNull()
    expect(channelFieldIsCleared({ ...form, clearSecrets: ["auth"] }, "auth.password")).toBe(true)
    expect(channelFieldPresent({ ...form, present: { "named_secrets.key": true } }, "named_secrets")).toBe(true)
  })

  it("serializes each provider with typed JSON and safe defaults", () => {
    for (const channelType of notificationChannelTypes) {
      const form = emptyAlertChannelForm(channelType)
      form.name = channelType
      for (const field of notificationFields[channelType]) {
        if (field.required) form.values[field.key] = field.kind === "number" ? "587" : field.key === "to" ? "ops@example.test, team@example.test" : field.kind === "url" ? "https://notify.example.test" : "configured"
      }
      const payload = alertChannelPayload(form)
      expect(typeof payload.config).toBe("object")
      expect(payload.channel_type).toBe(channelType)
      expect(alertChannelFormError(form)).toBeNull()
    }
    const form = emptyAlertChannelForm("custom_http")
    form.values = { ...form.values, endpoint: "https://notify.example.test", headers: '{"Authorization":"{{secret.key}}"}', query: '{"topic":"ops"}', named_secrets: '{"key":"test-value"}', "auth.mode": "basic", "auth.username": "user", "auth.password": " pass ", "hmac.secret": "signing", "hmac.header": "X-Signature", body: '{"message":"{{event.message}}"}' }
    expect(serializeAlertChannelConfig(form)).toMatchObject({ method: "POST", timeout_seconds: 15, auth: { mode: "basic", username: "user", password: " pass " }, hmac: { secret: "signing", header: "X-Signature" }, body: { message: "{{event.message}}" } })
    form.values.body_mode = "text"; form.values.body = "plain text"
    expect(serializeAlertChannelConfig(form).body).toBe("plain text")
    const turbo = emptyAlertChannelForm("serverchan_turbo"); turbo.values.noip = "true"
    expect(serializeAlertChannelConfig(turbo).noip).toBe(true)
  })

  it("requires actual saved values before permitting blank required fields", () => {
    const form = emptyAlertChannelForm("telegram"); form.name = "Telegram"
    expect(alertChannelFormError(form)).toBe("notification.required_fields")
    form.present = { bot_token: true, chat_id: true }
    expect(alertChannelFormError(form)).toBeNull()
    const saved = alertChannelFormFrom(notificationChannel({ channel_type: "serverchan_3", config: { tags: ["ops", "prod"] } }))
    expect(saved.values.tags).toBe("ops, prod")
  })

  it("rejects malformed or oversized configurations before sending", () => {
    const valid = emptyAlertChannelForm("custom_http"); valid.name = "Custom"; valid.values.endpoint = "https://notify.example.test"
    expect(alertChannelFormError({ ...valid, name: "" })).toBe("notification.invalid_name")
    expect(alertChannelFormError({ ...valid, name: "x".repeat(81) })).toBe("notification.invalid_name")
    for (const endpoint of ["bad", "file:///tmp/a", "https://user:password@example.test", "https://example.test/#fragment"]) {
      expect(alertChannelFormError({ ...valid, values: { ...valid.values, endpoint } })).toBe("notification.invalid_url")
    }
    for (const raw of ["{", "[]", "null", '{"value":2}', JSON.stringify(Object.fromEntries(Array.from({ length: 33 }, (_, i) => [i, "x"])))]) {
      expect(alertChannelFormError({ ...valid, values: { ...valid.values, headers: raw } })).not.toBeNull()
    }
    for (const [key, value] of [["timeout_seconds", "31"], ["timeout_seconds", "bad"], ["redirects", "4"], ["redirects", "-1"], ["body", "{"]]) {
      expect(alertChannelFormError({ ...valid, values: { ...valid.values, [key]: value } })).not.toBeNull()
    }
    expect(alertChannelFormError({ ...valid, templateSubject: "x".repeat(201) })).toBe("notification.too_large")
    expect(alertChannelFormError({ ...valid, template: "x".repeat(16_385) })).toBe("notification.too_large")
    expect(alertChannelFormError({ ...valid, values: { ...valid.values, endpoint: `https://example.test/${"a".repeat(16_385)}` } })).toBe("notification.too_large")
    const email = emptyAlertChannelForm("email"); email.name = "Mail"; email.enabled = false; email.values.port = "65536"
    expect(alertChannelFormError(email)).toBe("notification.invalid_number")
  })
})
