import { describe, expect, it } from "vitest"
import {
  alertChannelFormError,
  alertChannelFormFrom,
  emptyAlertChannelForm,
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
})
