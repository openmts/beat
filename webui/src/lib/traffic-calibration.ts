import { api } from "@/lib/api-client"

/** 校准事件只可能是 set（声明账面目标）或 revoke（回到原始 MTS）。 */
export type TrafficCalibrationKind = "set" | "revoke"

export interface TrafficCalibrationActor {
  id: string
  name: string
  session_id: string
  request_id: string
}

export interface TrafficCalibrationEvent {
  id: string
  node_id: string | null
  node_name: string
  kind: TrafficCalibrationKind
  effective_at?: string
  target_sent?: string
  target_received?: string
  reason: string
  actor: TrafficCalibrationActor
  reference_event_id?: string
  created_at: string
}

export interface TrafficCalibrationChange {
  event: TrafficCalibrationEvent
  revoked?: TrafficCalibrationEvent
  replayed: boolean
}

export interface TrafficCalibrationHistory {
  events: TrafficCalibrationEvent[]
  next_cursor?: string
}

export interface TrafficCalibrationPayload {
  request_id: string
  effective_at?: string
  target_sent: string
  target_received: string
  reason: string
}

const BINARY_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const

/** bytes 是服务端下发的规范十进制字节串；这里只做人类可读的二进制展示。 */
export function formatCalibrationBytes(value: string): string {
  if (!/^\d+$/.test(value)) return "--"
  let remaining = BigInt(value)
  if (remaining === 0n) return "0 B"
  const base = 1024n
  let unit = 0
  let whole = remaining
  let remainder = 0n
  while (unit < BINARY_UNITS.length - 1 && whole >= base) {
    remainder = whole % base
    whole /= base
    unit += 1
  }
  if (unit === 0) return `${whole} B`
  const fraction = (remainder * 10n) / base
  const rendered = fraction === 0n ? `${whole}` : `${whole}.${fraction}`
  return `${rendered} ${BINARY_UNITS[unit]}`
}

/**
 * 把人类可读的二进制单位输入解析成服务端要求的规范十进制字节串。
 *
 * 只接受非负整数或一位小数，例如 "1.5 GiB"、"512 MiB"、"1048576"。返回 undefined
 * 表示输入非法，调用方必须阻止提交而不是猜测一个值。
 */
export function parseCalibrationBytes(input: string): string | undefined {
  const trimmed = input.trim()
  if (trimmed === "") return undefined
  const match = /^(\d+)(?:\.(\d))?\s*([A-Za-z]+)?$/.exec(trimmed)
  if (!match) return undefined
  const unit = (match[3] ?? "B").toLowerCase()
  const index = BINARY_UNITS.findIndex((candidate) => candidate.toLowerCase() === unit)
  if (index < 0) return undefined
  const scale = 1024n ** BigInt(index)
  const fractionDigit = match[2] === undefined ? 0n : BigInt(match[2])
  const bytes = (BigInt(match[1]) * 10n + fractionDigit) * scale / 10n
  return bytes.toString()
}

export function isCanonicalCalibrationBytes(value: string): boolean {
  return /^(0|[1-9]\d*)$/.test(value)
}

export function newCalibrationRequestID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID()
  }
  const bytes = new Uint8Array(16)
  for (let index = 0; index < bytes.length; index += 1) bytes[index] = Math.floor(Math.random() * 256)
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`
}

function nodePath(nodeID: string): string {
  return `/admin/nodes/${encodeURIComponent(nodeID)}/traffic-calibrations`
}

export async function listTrafficCalibrations(
  nodeID: string,
  options: { limit?: number; cursor?: string; signal?: AbortSignal } = {},
): Promise<TrafficCalibrationHistory> {
  return (await api.get<TrafficCalibrationHistory>(nodePath(nodeID), {
    params: { limit: options.limit, cursor: options.cursor },
    signal: options.signal,
  })).data
}

export async function createTrafficCalibration(
  nodeID: string,
  payload: TrafficCalibrationPayload,
): Promise<TrafficCalibrationChange> {
  return (await api.post<TrafficCalibrationChange>(nodePath(nodeID), payload)).data
}

export async function replaceTrafficCalibration(
  nodeID: string,
  eventID: string,
  payload: TrafficCalibrationPayload,
): Promise<TrafficCalibrationChange> {
  return (await api.post<TrafficCalibrationChange>(
    `${nodePath(nodeID)}/${encodeURIComponent(eventID)}/replace`,
    payload,
  )).data
}

export async function revokeTrafficCalibration(
  nodeID: string,
  eventID: string,
  requestID: string,
): Promise<TrafficCalibrationChange> {
  return (await api.post<TrafficCalibrationChange>(
    `${nodePath(nodeID)}/${encodeURIComponent(eventID)}/revoke`,
    { request_id: requestID },
  )).data
}

/** 当前生效事件：新到旧的追加历史里第一条状态，revoke 之后回到原始 MTS。 */
export function activeCalibration(events: TrafficCalibrationEvent[]): TrafficCalibrationEvent | undefined {
  return events.find((event) => event.kind === "set")
}
