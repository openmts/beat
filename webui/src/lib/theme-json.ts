import type { ThemeJSON } from "@/types/theme-runtime"

export class ThemeDataError extends Error {
  readonly code: "snapshot_budget" | "invalid_data"
  constructor(code: "snapshot_budget" | "invalid_data") { super(code); this.code = code }
}

const FORBIDDEN_KEYS = new Set(["__proto__", "constructor", "prototype", "agent_token", "ssh_private_key", "ssh_public_key", "private_remark", "password", "access_token", "refresh_token", "csrf_token", "session_cookie", "authorization"])

export function plainRecord(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false
  const prototype = Object.getPrototypeOf(value)
  return prototype === Object.prototype || prototype === null
}

export function jsonBytes(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).byteLength
}

export function boundedJSON(value: unknown, limits: { depth: number; keys: number; bytes: number }): ThemeJSON {
  const stack = [{ value, depth: 0 }]
  let keys = 0
  let characters = 0
  while (stack.length) {
    const current = stack.pop()!
    if (current.depth > limits.depth) throw new ThemeDataError("snapshot_budget")
    const candidate = current.value
    if (typeof candidate === "string") {
      characters += candidate.length
      if (characters > limits.bytes) throw new ThemeDataError("snapshot_budget")
      continue
    }
    if (candidate === null || typeof candidate === "boolean") continue
    if (typeof candidate === "number" && Number.isFinite(candidate)) continue
    if (!Array.isArray(candidate) && !plainRecord(candidate)) throw new ThemeDataError("invalid_data")
    for (const [key, child] of Object.entries(candidate)) {
      if (FORBIDDEN_KEYS.has(key.toLowerCase())) throw new ThemeDataError("invalid_data")
      characters += key.length
      if (characters > limits.bytes) throw new ThemeDataError("snapshot_budget")
      if (++keys > limits.keys) throw new ThemeDataError("snapshot_budget")
      stack.push({ value: child, depth: current.depth + 1 })
    }
  }
  if (jsonBytes(value) > limits.bytes) throw new ThemeDataError("snapshot_budget")
  return value as ThemeJSON
}

export function publicConfiguration(raw = "{}"): ThemeJSON {
  if (raw.length > 16 * 1024 || new TextEncoder().encode(raw).byteLength > 16 * 1024) throw new ThemeDataError("snapshot_budget")
  let value: unknown
  try { value = JSON.parse(raw) } catch { throw new ThemeDataError("invalid_data") }
  if (!plainRecord(value)) throw new ThemeDataError("invalid_data")
  return boundedJSON(value, { depth: 5, keys: 256, bytes: 16 * 1024 })
}

export function publicText(value: unknown): string {
  if (value === undefined || value === null) return ""
  if (typeof value !== "string") throw new ThemeDataError("invalid_data")
  if (value.length > 8192) throw new ThemeDataError("snapshot_budget")
  return value
}

export function publicID(value: unknown): string {
  const id = publicText(value)
  if (!/^[a-zA-Z0-9_-]{1,128}$/.test(id)) throw new ThemeDataError("invalid_data")
  return id
}

export function finiteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value)
}

export function assertCount(values: readonly unknown[], maximum: number): void {
  if (values.length > maximum) throw new ThemeDataError("snapshot_budget")
}
