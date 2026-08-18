import { describe, expect, it } from "vitest"
import {
  alertChannelFormError,
  alertChannelFormFrom,
  emptyAlertChannelForm,
  headersFromString,
  headersToString,
  serializeAlertChannelConfig,
} from "@/pages/admin/alert-channel-config"

describe("alert channel config", () => {
  it("parses legacy webhook and sanitized secrets", () => {
    const webhook = alertChannelFormFrom({
      id: "w", name: "Webhook", channel_type: "webhook", config: "https://example.com/hook",
      enabled: true, created_at: "", updated_at: "",
    })
    expect(webhook.webhookURL).toBe("https://example.com/hook")

    const telegram = alertChannelFormFrom({
      id: "t", name: "Telegram", channel_type: "telegram",
      config: `{"bot_token":"","chat_id":"42","has_bot_token":true}`,
      enabled: true, created_at: "", updated_at: "",
    })
    expect(telegram.chatID).toBe("42")
    expect(telegram.botToken).toBe("")
  })

  it("serializes email recipients and validates forms", () => {
    const form = {
      ...emptyAlertChannelForm(), name: "Email", channelType: "email" as const,
      smtpHost: "smtp.example.com", smtpPort: "587", smtpFrom: "beat@example.com",
      smtpTo: "ops@example.com, owner@example.com", smtpSecurity: "starttls" as const,
    }
    expect(alertChannelFormError(form, false)).toBeNull()
    expect(JSON.parse(serializeAlertChannelConfig(form))).toMatchObject({
      port: 587, to: ["ops@example.com", "owner@example.com"], security: "starttls",
    })
    expect(alertChannelFormError({ ...form, smtpPort: "bad" }, false)).toBe("alert.invalid_smtp")
    expect(alertChannelFormError({ ...form, smtpTo: "bad" }, false)).toBe("alert.invalid_email")
  })

  it("requires new secrets but permits blank secrets while editing", () => {
    const telegram = {
      ...emptyAlertChannelForm(), name: "Telegram", channelType: "telegram" as const, chatID: "42",
    }
    expect(alertChannelFormError(telegram, false)).toBe("alert.invalid_telegram")
    expect(alertChannelFormError(telegram, true)).toBeNull()

    const email = {
      ...emptyAlertChannelForm(), name: "Email", channelType: "email" as const,
      smtpHost: "smtp.example.com", smtpPort: "587", smtpUsername: "beat",
      smtpFrom: "beat@example.com", smtpTo: "ops@example.com",
    }
    expect(alertChannelFormError(email, false)).toBe("alert.smtp_password_required")
    expect(alertChannelFormError(email, true)).toBeNull()
  })

  it("serializes and restores custom push templates", () => {
    const webhook = {
      ...emptyAlertChannelForm(), name: "Webhook", channelType: "webhook" as const,
      webhookURL: "https://example.com/hook", template: "[{{severity}}] {{rule}}",
    }
    const serialized = serializeAlertChannelConfig(webhook)
    expect(JSON.parse(serialized)).toMatchObject({
      url: "https://example.com/hook", template: "[{{severity}}] {{rule}}",
    })
    const restored = alertChannelFormFrom({
      id: "w", name: "Webhook", channel_type: "webhook", config: serialized,
      enabled: true, created_at: "", updated_at: "",
    })
    expect(restored.template).toBe("[{{severity}}] {{rule}}")

    const telegram = {
      ...emptyAlertChannelForm(), name: "Telegram", channelType: "telegram" as const,
      chatID: "42", template: "node {{node}}",
    }
    expect(JSON.parse(serializeAlertChannelConfig(telegram))).toMatchObject({
      chat_id: "42", template: "node {{node}}",
    })

    const plainWebhook = {
      ...emptyAlertChannelForm(), name: "Webhook", channelType: "webhook" as const,
      webhookURL: "https://example.com/hook", template: "",
    }
    const plain = JSON.parse(serializeAlertChannelConfig(plainWebhook))
    expect(plain.template).toBeUndefined()
  })

  it("round-trips webhook custom headers", () => {
    const form = {
      ...emptyAlertChannelForm(), name: "Hook", channelType: "webhook" as const,
      webhookURL: "https://example.com/hook",
      webhookHeaders: "Authorization: Bearer token\nX-Custom: abc",
    }
    const serialized = JSON.parse(serializeAlertChannelConfig(form))
    expect(serialized.headers).toEqual({
      Authorization: "Bearer token",
      "X-Custom": "abc",
    })
    const restored = alertChannelFormFrom({
      id: "w", name: "Hook", channel_type: "webhook", config: JSON.stringify(serialized),
      enabled: true, created_at: "", updated_at: "",
    })
    expect(restored.webhookHeaders).toBe("Authorization: Bearer token\nX-Custom: abc")
  })

  it("omits headers when empty and parses helpers", () => {
    const form = {
      ...emptyAlertChannelForm(), name: "Hook", channelType: "webhook" as const,
      webhookURL: "https://example.com/hook", webhookHeaders: "  \n",
    }
    const serialized = JSON.parse(serializeAlertChannelConfig(form))
    expect(serialized.headers).toBeUndefined()

    expect(headersToString({ A: "1" })).toBe("A: 1")
    expect(headersFromString("A: 1\nB: 2\n\nbadline")).toEqual({ A: "1", B: "2" })
    expect(headersFromString("")).toEqual({})
  })
})
