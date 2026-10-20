import { MAX_THEME_MESSAGE_BYTES, THEME_PROTOCOL, type ThemeFrameMessage } from "@/types/theme-runtime"
import { boundedJSON, plainRecord } from "@/lib/theme-json"

interface FrameIdentity { window: unknown; instance: string }
interface FrameEvent { source: unknown; data: unknown }

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
  if (message.type === "error" && exactKeys(message.payload, ["code"])) {
    const code = message.payload.code
    if (code === "render_failed" || code === "unsupported_data" || code === "runtime_error") return { type: "error", payload: { code } }
  }
  return null
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
