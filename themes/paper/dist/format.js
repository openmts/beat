import { strings } from "./i18n.js"
import { state } from "./state.js"

const numberFormats = new Map()

function numberFormat(digits) {
  const key = `${state.locale}:${digits}`
  let formatter = numberFormats.get(key)
  if (!formatter) {
    formatter = new Intl.NumberFormat(state.locale, { minimumFractionDigits: digits, maximumFractionDigits: digits })
    numberFormats.set(key, formatter)
  }
  return formatter
}

export function decimal(value, digits = 0) {
  return Number.isFinite(value) ? numberFormat(digits).format(value) : "—"
}

export function formatBytes(value) {
  if (!Number.isFinite(value) || value < 0) return "—"
  if (value === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB", "PB"]
  let scaled = value
  let unit = 0
  while (scaled >= 1024 && unit < units.length - 1) {
    scaled /= 1024
    unit += 1
  }
  const digits = scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2
  return `${decimal(scaled, digits)} ${units[unit]}`
}

export function formatRate(value) {
  const bytes = formatBytes(value)
  return bytes === "—" ? bytes : `${bytes}/s`
}

export function percentValue(value) {
  return Number.isFinite(value) ? `${decimal(value, 1)}%` : "—"
}

export function clampPercent(value) {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : 0
}

export function formatDuration(seconds, locale = state.locale) {
  if (!Number.isFinite(seconds) || seconds < 0) return ""
  const units = strings(locale).duration
  let remaining = Math.floor(seconds)
  const days = Math.floor(remaining / 86400)
  remaining %= 86400
  const hours = Math.floor(remaining / 3600)
  remaining %= 3600
  const minutes = Math.floor(remaining / 60)
  const parts = []
  if (days) parts.push(`${days}${units.day}`)
  if (hours) parts.push(`${hours}${units.hour}`)
  if (!days && minutes) parts.push(`${minutes}${units.minute}`)
  if (!parts.length) parts.push(`${Math.max(0, remaining)}${units.second}`)
  return parts.join(" ")
}

function validDate(value) {
  const timestamp = typeof value === "number" ? value : Date.parse(value)
  return Number.isFinite(timestamp) ? new Date(timestamp) : null
}

export function formatDate(value, options, locale = state.locale) {
  const date = validDate(value)
  if (!date) return strings(locale).unknown
  try {
    return new Intl.DateTimeFormat(locale, options).format(date)
  } catch {
    return date.toLocaleString()
  }
}

export function shortDate(value, locale = state.locale) {
  return formatDate(value, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }, locale)
}
