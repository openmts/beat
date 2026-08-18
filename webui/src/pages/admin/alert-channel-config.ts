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
  template: string
}

interface WebhookConfig {
  url?: string
  template?: string
}

interface TelegramConfig {
  bot_token?: string
  chat_id?: string
  template?: string
}

interface EmailConfig {
  host?: string
  port?: number
  username?: string
  password?: string
  from?: string
  to?: string[]
  security?: EmailSecurity
  template?: string
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
    template: "",
  }
}

export function alertChannelFormFrom(channel: AlertChannel): AlertChannelFormState {
  const form = { ...emptyAlertChannelForm(), name: channel.name, channelType: channel.channel_type }
  if (channel.channel_type === "webhook") {
    const config = parseWebhookConfig(channel.config)
    form.webhookURL = config.url ?? ""
    form.template = config.template ?? ""
  } else if (channel.channel_type === "telegram") {
    const config = parseJSON<TelegramConfig>(channel.config)
    form.botToken = config.bot_token ?? ""
    form.chatID = config.chat_id ?? ""
    form.template = config.template ?? ""
  } else {
    const config = parseJSON<EmailConfig>(channel.config)
    form.smtpHost = config.host ?? ""
    form.smtpPort = String(config.port ?? 587)
    form.smtpUsername = config.username ?? ""
    form.smtpPassword = config.password ?? ""
    form.smtpFrom = config.from ?? ""
    form.smtpTo = config.to?.join(", ") ?? ""
    form.smtpSecurity = config.security ?? "starttls"
    form.template = config.template ?? ""
  }
  return form
}

export function serializeAlertChannelConfig(form: AlertChannelFormState): string {
  if (form.channelType === "webhook") {
    const config: Record<string, string> = { url: form.webhookURL.trim() }
    if (form.template.trim()) config.template = form.template.trim()
    return JSON.stringify(config)
  }
  if (form.channelType === "telegram") {
    const config: Record<string, string> = { bot_token: form.botToken.trim(), chat_id: form.chatID.trim() }
    if (form.template.trim()) config.template = form.template.trim()
    return JSON.stringify(config)
  }
  const config: Record<string, unknown> = {
    host: form.smtpHost.trim(),
    port: Number(form.smtpPort),
    username: form.smtpUsername.trim(),
    password: form.smtpPassword,
    from: form.smtpFrom.trim(),
    to: splitRecipients(form.smtpTo),
    security: form.smtpSecurity,
  }
  if (form.template.trim()) config.template = form.template.trim()
  return JSON.stringify(config)
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

function parseWebhookConfig(raw: string): WebhookConfig {
  if (raw.startsWith("http://") || raw.startsWith("https://")) {
    return { url: raw }
  }
  return parseJSON<WebhookConfig>(raw)
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
