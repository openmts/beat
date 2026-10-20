/** 时间表达式：Grafana 风格的相对表达式与 UTC 绝对时间，最终都收敛成工作区使用的 ISO 秒串。 */
export type WorkspaceTimeExpression = { value: string } | { error: "workspace.expression_invalid" }

const relativePattern = /^now(?:-(\d{1,5})([smhdw]))?(?:\/([smhdw]))?$/
const absolutePattern = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?Z?$/
const unitSeconds: Record<string, number> = { s: 1, m: 60, h: 3600, d: 86400, w: 604800 }
// 表达式只服务于查询窗口（服务端上限 31 天），十年以上直接判定无效，避免溢出成意外时间点。
const maximumOffsetSeconds = 10 * 365 * 86400
const invalid = { error: "workspace.expression_invalid" } as const

/** 工作区的时间串统一是秒精度 UTC：`2026-10-17T09:00:00Z`。 */
export function formatWorkspaceInstant(milliseconds: number): string {
  return new Date(milliseconds).toISOString().replace(/\.\d{3}Z$/, "Z")
}

export function parseWorkspaceTimeExpression(text: string, now = Date.now()): WorkspaceTimeExpression {
  const value = text.trim()
  const relative = relativePattern.exec(value)
  if (relative) return relativeExpression(relative, now)
  const absolute = absolutePattern.exec(value)
  return absolute ? absoluteExpression(absolute) : invalid
}

function relativeExpression(match: RegExpExecArray, now: number): WorkspaceTimeExpression {
  const offset = match[1] ? Number(match[1]) * unitSeconds[match[2]!] : 0
  if (!Number.isFinite(offset) || offset > maximumOffsetSeconds) return invalid
  const instant = now - offset * 1000
  return { value: formatWorkspaceInstant(match[3] ? alignWorkspaceInstant(instant, match[3]) : instant) }
}

function absoluteExpression(match: RegExpExecArray): WorkspaceTimeExpression {
  const [, year, month, day, hour = "0", minute = "0", second = "0"] = match
  const clock = [Number(hour), Number(minute), Number(second)]
  if (clock[0]! > 23 || clock[1]! > 59 || clock[2]! > 59) return invalid
  const parsed = Date.UTC(Number(year), Number(month) - 1, Number(day), ...(clock as [number, number, number]))
  const stamp = new Date(parsed)
  // Date.UTC 会把 2 月 31 日滚动到 3 月，回读三段才能把这种输入判成无效。
  const rolled = stamp.getUTCMonth() !== Number(month) - 1 || stamp.getUTCDate() !== Number(day) || stamp.getUTCFullYear() !== Number(year)
  return rolled ? invalid : { value: formatWorkspaceInstant(parsed) }
}

/** 对齐到 UTC 的整秒/整分/整时/整天/整周（周一为起点）。 */
function alignWorkspaceInstant(milliseconds: number, unit: string): number {
  const seconds = Math.floor(milliseconds / 1000)
  if (unit === "s") return seconds * 1000
  if (unit === "m") return Math.floor(seconds / 60) * 60 * 1000
  if (unit === "h") return Math.floor(seconds / 3600) * 3600 * 1000
  if (unit === "w") return (Math.floor((seconds + 3 * 86400) / (7 * 86400)) * 7 - 3) * 86400 * 1000
  return Math.floor(seconds / 86400) * 86400 * 1000
}
