import type { NetworkProbeType } from "@/types"

export type NetworkTargetFormatError = "icmp" | "tcp" | "http"

/** 切换探测协议时，把已有的 HTTP/TCP/ICMP 目标转换为新协议可直接使用的形式。 */
export function normalizeTargetForType(type: NetworkProbeType, value: string): string {
  const target = normalizePunctuation(value)
  if (!target) return ""
  const parsed = parseHTTPURL(target)
  if (type === "http") {
    if (parsed) return target
    if (target.includes("://")) return target
    return `http://${target}${hasPort(target) ? "" : "/"}`
  }
  if (type === "tcp") {
    if (parsed) return hostPort(parsed.hostname, parsed.port || (parsed.protocol === "https:" ? "443" : "80"))
    return target
  }
  if (parsed) return parsed.hostname
  const host = stripPort(target)
  return host ?? target
}

/** 与服务端保持一致的目标格式判断，用于提交前给出明确提示。 */
export function networkTargetFormatError(type: NetworkProbeType, value: string): NetworkTargetFormatError | null {
  const target = normalizePunctuation(value)
  if (!target) return null
  if (type === "icmp") return target.includes(":") ? "icmp" : null
  if (type === "tcp") return parseHostPort(target) ? null : "tcp"
  const parsed = parseHTTPURL(target)
  return parsed && !parsed.username && !parsed.password && !parsed.hash ? null : "http"
}

/** 中文输入法常产生全角冒号；不可见零宽字符也会让目标看起来正确但校验失败。 */
function normalizePunctuation(value: string): string {
  return value
    .replaceAll("：", ":")
    .replace(/[\u200B-\u200D\uFEFF]/g, "")
    .trim()
}

function parseHTTPURL(value: string): URL | null {
  if (!/^https?:\/\//i.test(value)) return null
  try {
    const parsed = new URL(value)
    return parsed.hostname ? parsed : null
  } catch {
    return null
  }
}

function parseHostPort(value: string): URL | null {
  if (value.includes("://") || value.includes("/") || value.includes("?") || value.includes("#")) return null
  try {
    const parsed = new URL(`tcp://${value}`)
    return parsed.hostname && parsed.port ? parsed : null
  } catch {
    return null
  }
}

function hostPort(hostname: string, port: string): string {
  return hostname.includes(":") ? `[${hostname}]:${port}` : `${hostname}:${port}`
}

function hasPort(value: string): boolean {
  return parseHostPort(value) !== null
}

function stripPort(value: string): string | null {
  if (value.startsWith("[")) {
    const end = value.indexOf("]")
    return end > 1 ? value.slice(1, end) : null
  }
  const parts = value.split(":")
  return parts.length === 2 ? parts[0] : null
}
