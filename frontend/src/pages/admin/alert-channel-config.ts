import type { AlertChannel, AlertChannelType } from "@/types"

export type EmailSecurity = "starttls" | "tls" | "none"

export interface AlertChannelFormState {
  name: string
  channelType: AlertChannelType
  webhookURL: string
  botToken: string
  chatID: string
  smtpHost: string
  smtpPort: string
  smtpUsername: string
  smtpPassword: string
  smtpFrom: string
  smtpTo: string
  smtpSecurity: EmailSecurity
}

interface WebhookConfig {
  url?: string
}

interface TelegramConfig {
  bot_token?: string
  chat_id?: string
}

interface EmailConfig {
  host?: string
  port?: number
  username?: string
  password?: string
  from?: string
  to?: string[]
  security?: EmailSecurity
}

export function emptyAlertChannelForm(): AlertChannelFormState {
  return {
    name: "",
    channelType: "webhook",
    webhookURL: "",
    botToken: "",
    chatID: "",
    smtpHost: "",
    smtpPort: "587",
    smtpUsername: "",
    smtpPassword: "",
    smtpFrom: "",
    smtpTo: "",
    smtpSecurity: "starttls",
  }
}

export function alertChannelFormFrom(channel: AlertChannel): AlertChannelFormState {
  const form = { ...emptyAlertChannelForm(), name: channel.name, channelType: channel.channel_type }
  if (channel.channel_type === "webhook") {
    form.webhookURL = parseWebhook(channel.config)
  } else if (channel.channel_type === "telegram") {
    const config = parseJSON<TelegramConfig>(channel.config)
    form.botToken = config.bot_token ?? ""
    form.chatID = config.chat_id ?? ""
  } else {
    const config = parseJSON<EmailConfig>(channel.config)
    form.smtpHost = config.host ?? ""
    form.smtpPort = String(config.port ?? 587)
    form.smtpUsername = config.username ?? ""
    form.smtpPassword = config.password ?? ""
    form.smtpFrom = config.from ?? ""
    form.smtpTo = config.to?.join(", ") ?? ""
    form.smtpSecurity = config.security ?? "starttls"
  }
  return form
}

export function serializeAlertChannelConfig(form: AlertChannelFormState): string {
  if (form.channelType === "webhook") {
    return JSON.stringify({ url: form.webhookURL.trim() })
  }
  if (form.channelType === "telegram") {
    return JSON.stringify({ bot_token: form.botToken.trim(), chat_id: form.chatID.trim() })
  }
  return JSON.stringify({
    host: form.smtpHost.trim(),
    port: Number(form.smtpPort),
    username: form.smtpUsername.trim(),
    password: form.smtpPassword,
    from: form.smtpFrom.trim(),
    to: splitRecipients(form.smtpTo),
    security: form.smtpSecurity,
  })
}

export function alertChannelFormError(
  form: AlertChannelFormState,
  editing: boolean,
): string | null {
  if (!form.name.trim()) return "alert.channel_name_required"
  if (form.channelType === "webhook") {
    return validHTTPURL(form.webhookURL) ? null : "alert.invalid_webhook"
  }
  if (form.channelType === "telegram") {
    if (!form.chatID.trim() || (!editing && !form.botToken.trim())) return "alert.invalid_telegram"
    return null
  }
  const port = Number(form.smtpPort)
  if (!form.smtpHost.trim() || !Number.isInteger(port) || port < 1 || port > 65535) {
    return "alert.invalid_smtp"
  }
  if (!validEmail(form.smtpFrom) || splitRecipients(form.smtpTo).some((value) => !validEmail(value))) {
    return "alert.invalid_email"
  }
  if (splitRecipients(form.smtpTo).length === 0) return "alert.invalid_email"
  if (form.smtpUsername.trim() && !editing && !form.smtpPassword) return "alert.smtp_password_required"
  return null
}

function parseWebhook(raw: string) {
  if (raw.startsWith("http://") || raw.startsWith("https://")) return raw
  return parseJSON<WebhookConfig>(raw).url ?? ""
}

function parseJSON<T>(raw: string): T {
  try {
    return JSON.parse(raw) as T
  } catch {
    return {} as T
  }
}

function validHTTPURL(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === "http:" || url.protocol === "https:"
  } catch {
    return false
  }
}

function validEmail(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim())
}

function splitRecipients(value: string) {
  return value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean)
}
