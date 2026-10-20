import type { NotificationChannelType } from "@/types/notification"

export type ChannelFieldKind = "text" | "secret" | "url" | "number" | "choice" | "boolean" | "list" | "map" | "body"
export interface ChannelField {
  key: string
  kind: ChannelFieldKind
  secret?: boolean
  required?: boolean
  choices?: string[]
  initial?: string
}

const endpoint: ChannelField = { key: "endpoint", kind: "url", secret: true, required: true }
const headers: ChannelField = { key: "headers", kind: "map", secret: true }
const optionalEndpoint = { ...endpoint, required: false }

export const notificationChannelTypes: NotificationChannelType[] = ["webhook", "telegram", "email", "bark", "serverchan_turbo", "serverchan_3", "custom_http"]

export const notificationFields: Record<NotificationChannelType, ChannelField[]> = {
  webhook: [endpoint, headers],
  telegram: [
    { key: "bot_token", kind: "secret", secret: true, required: true },
    { key: "chat_id", kind: "secret", secret: true, required: true },
    { key: "parse_mode", kind: "choice", choices: ["", "HTML", "MarkdownV2"] }, optionalEndpoint,
  ],
  email: [
    { key: "host", kind: "text", secret: true, required: true },
    { key: "port", kind: "number", initial: "587", required: true },
    { key: "security", kind: "choice", choices: ["starttls", "tls", "none"], initial: "starttls" },
    { key: "username", kind: "secret", secret: true }, { key: "password", kind: "secret", secret: true },
    { key: "from", kind: "secret", secret: true, required: true }, { key: "to", kind: "list", secret: true, required: true },
  ],
  bark: [optionalEndpoint, { key: "device_key", kind: "secret", secret: true, required: true },
    { key: "level", kind: "choice", choices: ["", "active", "timeSensitive", "passive", "critical"] },
    { key: "group", kind: "text" }, { key: "sound", kind: "text" },
    { key: "icon", kind: "url", secret: true }, { key: "url", kind: "url", secret: true },
    { key: "compatibility", kind: "choice", choices: ["strict", "http_2xx"], initial: "strict" },
  ],
  serverchan_turbo: [
    { key: "send_key", kind: "secret", secret: true, required: true }, { key: "open_id", kind: "secret", secret: true },
    { key: "channel", kind: "text" }, { key: "noip", kind: "boolean", initial: "false" },
  ],
  serverchan_3: [endpoint, { key: "tags", kind: "list" }],
  custom_http: [endpoint, { key: "method", kind: "choice", choices: ["GET", "POST"], initial: "POST" }, headers,
    { key: "query", kind: "map", secret: true },
    { key: "body_mode", kind: "choice", choices: ["json", "form", "text"], initial: "json" },
    { key: "body", kind: "body", secret: true }, { key: "named_secrets", kind: "map", secret: true },
    { key: "auth.mode", kind: "choice", choices: ["", "none", "basic", "bearer"], initial: "none" },
    { key: "auth.username", kind: "secret", secret: true }, { key: "auth.password", kind: "secret", secret: true },
    { key: "auth.token", kind: "secret", secret: true },
    { key: "hmac.secret", kind: "secret", secret: true }, { key: "hmac.header", kind: "text" }, { key: "hmac.timestamp_header", kind: "text" },
    { key: "timeout_seconds", kind: "number", initial: "15" }, { key: "redirects", kind: "number", initial: "0" },
  ],
}

export const notificationEventKinds = ["alert.triggered", "alert.acknowledged", "alert.resolved", "alert.no_data", "alert.evaluation_error", "alert.evaluation_recovered", "availability.offline", "availability.recovered", "node.expiring", "node.expired", "node.renewed", "traffic.quota", "traffic.report", "fleet.status_summary", "notification.delivery_failed"]

export function fieldLabelKey(key: string) { return `notification.field.${key.replaceAll(".", "_")}` }
