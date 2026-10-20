import type { AlertChannel, AlertChannelType } from "@/types"
import type { NotificationChannelWrite } from "@/types/notification"
import { notificationEventKinds, notificationFields, type ChannelField } from "@/lib/notification-fields"

export interface AlertChannelFormState {
  name: string
  channelType: AlertChannelType
  enabled: boolean
  revision: number
  values: Record<string, string>
  present: Record<string, boolean>
  clearSecrets: string[]
  eventKinds: string[]
  templateSubject: string
  template: string
}

export function emptyAlertChannelForm(channelType: AlertChannelType = "webhook"): AlertChannelFormState {
  const values = Object.fromEntries(notificationFields[channelType].map(field => [field.key, field.initial ?? ""]))
  return { name: "", channelType, enabled: true, revision: 0, values, present: {}, clearSecrets: [],
    eventKinds: [...notificationEventKinds], templateSubject: "", template: "" }
}

export function alertChannelFormFrom(channel: AlertChannel): AlertChannelFormState {
  const form = emptyAlertChannelForm(channel.channel_type)
  for (const field of notificationFields[channel.channel_type]) {
    if (field.secret || field.key.includes(".")) { form.values[field.key] = ""; continue }
    const value = channel.config[field.key]
    if (value !== undefined) form.values[field.key] = Array.isArray(value) ? value.join(", ") : String(value)
  }
  return { ...form, name: channel.name, enabled: channel.enabled, revision: channel.current_revision,
    present: { ...channel.secret_present }, eventKinds: [...channel.event_kinds],
    templateSubject: channel.template_subject, template: channel.template_text }
}

export function channelFieldIsCleared(form: AlertChannelFormState, key: string) {
  return form.clearSecrets.some(path => key === path || key.startsWith(`${path}.`))
}

export function channelFieldPresent(form: AlertChannelFormState, key: string) {
  return Boolean(form.present[key] || Object.keys(form.present).some(path => path.startsWith(`${key}.`) && form.present[path]))
}

export function serializeAlertChannelConfig(form: AlertChannelFormState): Record<string, unknown> {
  const config: Record<string, unknown> = {}
  for (const field of notificationFields[form.channelType]) {
    const raw = form.values[field.key] ?? ""
    if (channelFieldIsCleared(form, field.key)) continue
    if (!raw.trim() && (field.secret || field.key.includes(".") || field.kind === "number")) continue
    writePath(config, field.key, convertField(field, raw, form))
  }
  return config
}

export function alertChannelPayload(form: AlertChannelFormState): NotificationChannelWrite {
  return { name: form.name.trim(), channel_type: form.channelType, enabled: form.enabled, expected_revision: form.revision,
    config: serializeAlertChannelConfig(form), event_kinds: form.eventKinds, clear_secrets: form.clearSecrets,
    template_subject: form.templateSubject, template_text: form.template }
}

function convertField(field: ChannelField, raw: string, form: AlertChannelFormState): unknown {
  if (field.kind === "number") {
    const value = Number(raw)
    if (!Number.isInteger(value) || value < 0) throw new Error("notification.invalid_number")
    return value
  }
  if (field.kind === "boolean") return raw === "true"
  if (field.kind === "list") return raw.split(/[,\n]/).map(value => value.trim()).filter(Boolean)
  if (field.kind === "map") return parseStringMap(raw, field.key === "named_secrets" ? 8 : 32)
  if (field.kind === "body") {
    if (form.values.body_mode === "text") return raw
    try { return JSON.parse(raw) as unknown } catch { throw new Error("notification.invalid_json") }
  }
  return field.kind === "secret" ? raw : raw.trim()
}

function parseStringMap(raw: string, limit: number): Record<string, string> {
  let value: unknown
  try { value = JSON.parse(raw) as unknown } catch { throw new Error("notification.invalid_json") }
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("notification.invalid_map")
  const entries = Object.entries(value)
  if (entries.length > limit || entries.some(([key, entry]) => !key || typeof entry !== "string")) throw new Error("notification.invalid_map")
  return Object.fromEntries(entries) as Record<string, string>
}

function writePath(target: Record<string, unknown>, key: string, value: unknown) {
  const [parent, child] = key.split(".")
  if (!child) { target[parent] = value; return }
  const group = (target[parent] ?? {}) as Record<string, unknown>
  group[child] = value
  target[parent] = group
}

export function alertChannelFormError(form: AlertChannelFormState): string | null {
  if (!form.name.trim() || form.name.trim().length > 80) return "notification.invalid_name"
  for (const field of notificationFields[form.channelType]) {
    const value = channelFieldIsCleared(form, field.key) ? "" : form.values[field.key] ?? ""
    const kept = channelFieldPresent(form, field.key) && !channelFieldIsCleared(form, field.key)
    if (form.enabled && field.required && !value.trim() && !kept) return "notification.required_fields"
    if (field.kind === "url" && value.trim() && !validHTTPURL(value)) return "notification.invalid_url"
  }
  try {
    const payload = alertChannelPayload(form)
    const bytes = (text: string) => new TextEncoder().encode(text).length
    if (bytes(payload.template_subject) > 200 || bytes(payload.template_text) > 16_384 || bytes(JSON.stringify(payload.config)) > 16_384) return "notification.too_large"
    const port = payload.config.port as number | undefined
    if (port !== undefined && (port < 1 || port > 65_535)) return "notification.invalid_number"
    const timeout = payload.config.timeout_seconds as number | undefined
    if (timeout !== undefined && (timeout < 1 || timeout > 30)) return "notification.invalid_number"
    const redirects = payload.config.redirects as number | undefined
    if (redirects !== undefined && redirects > 3) return "notification.invalid_number"
  } catch (error) { return error instanceof Error ? error.message : "notification.invalid_json" }
  return null
}

function validHTTPURL(value: string) {
  try {
    const url = new URL(value)
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password && !url.hash && !/[\r\n\t]/.test(value)
  } catch { return false }
}
