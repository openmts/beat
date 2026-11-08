import { MAX_THEME_MESSAGE_BYTES, THEME_PROTOCOL, THEME_RELATIVE_RANGE_SECONDS, type ThemeFrameMessage, type ThemeRange } from "@/types/theme-runtime"
import { boundedJSON, plainRecord } from "@/lib/theme-json"

interface FrameIdentity { window: unknown; instance: string }
interface FrameEvent { source: unknown; data: unknown }
const MAX_ABSOLUTE_RANGE_MS = 31 * 24 * 60 * 60 * 1000

export function frameMessage(event: FrameEvent, frame: FrameIdentity, visible: ReadonlySet<string>): ThemeFrameMessage | null {
  if (!frame.window || event.source !== frame.window || !frame.instance) return null
  try {
    boundedJSON(event.data, { depth: 4, keys: 12, bytes: MAX_THEME_MESSAGE_BYTES })
    if (!plainRecord(event.data) || !exactKeys(event.data, ["beat"])) return null
    const message = event.data.beat
    if (!plainRecord(message) || message.version !== THEME_PROTOCOL || message.instance_id !== frame.instance) return null
    return parseMessage(message, visible)
  } catch { return null }
}

function parseMessage(message: Record<string, unknown>, visible: ReadonlySet<string>): ThemeFrameMessage | null {
  if (message.type === "ready" && exactKeys(message, ["version", "instance_id", "type"])) return { type: "ready" }
  if (!exactKeys(message, ["version", "instance_id", "type", "payload"]) || !plainRecord(message.payload)) return null
  if (message.type === "navigate" && exactKeys(message.payload, ["path"])) {
    const path = message.payload.path
    if (typeof path === "string" && allowedThemePath(path, visible)) return { type: "navigate", payload: { path } }
  }
  if (message.type === "setRange") {
    const range = parseThemeRange(message.payload)
    if (range) return { type: "setRange", payload: range }
  }
  if (message.type === "error" && exactKeys(message.payload, ["code"])) {
    const code = message.payload.code
    if (code === "render_failed" || code === "unsupported_data" || code === "runtime_error") return { type: "error", payload: { code } }
  }
  return null
}

function parseThemeRange(value: Record<string, unknown>): ThemeRange | null {
  if (value.mode === "relative" && exactKeys(value, ["mode", "seconds"])) {
    const seconds = value.seconds
    if (typeof seconds === "number" && THEME_RELATIVE_RANGE_SECONDS.includes(seconds as (typeof THEME_RELATIVE_RANGE_SECONDS)[number])) {
      return { mode: "relative", seconds }
    }
    return null
  }
  if (value.mode !== "absolute" || !exactKeys(value, ["mode", "from", "to"])) return null
  const from = parseTimestamp(value.from)
  const to = parseTimestamp(value.to)
  if (from === null || to === null || from >= to || to - from > MAX_ABSOLUTE_RANGE_MS) return null
  return { mode: "absolute", from: new Date(from).toISOString(), to: new Date(to).toISOString() }
}

function parseTimestamp(value: unknown): number | null {
  if (typeof value !== "string" || value.length === 0 || value.length > 64) return null
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}

function exactKeys(value: Record<string, unknown>, keys: string[]): boolean {
  return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
}

export function allowedThemePath(path: string, visible: ReadonlySet<string>): boolean {
  if (path === "/") return true
  if (!path.startsWith("/node/")) return false
  const id = path.slice("/node/".length)
  return /^[a-zA-Z0-9_-]{1,128}$/.test(id) && visible.has(id)
}

export function themeAssetURL(digest: string, entry: string): string {
  const parts = entry.split("/")
  if (!/^[0-9a-f]{64}$/.test(digest) || entry.length > 240 || parts.length > 8 ||
    parts.some((part) => !validAssetPart(part))) throw new Error("invalid_data")
  return `/themes/assets/${digest}/${parts.map(encodeURIComponent).join("/")}`
}

function validAssetPart(part: string): boolean {
  if (!part || part.startsWith(".") || part.includes("\\") || part.includes(":")) return false
  return Array.from(part).every((character) => character.charCodeAt(0) >= 32 && character.charCodeAt(0) !== 127)
}

export function newThemeInstance(): string {
  const bytes = new Uint8Array(16)
  window.crypto.getRandomValues(bytes)
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")
}
