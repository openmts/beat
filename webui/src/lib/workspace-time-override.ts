import type { DashboardPanel } from "@/types/metric-workspace"

/**
 * 面板级时间覆盖沿用 Grafana 的相对时间写法：`now` 或 `now-<数值><单位>`，
 * 单位是 s/m/h/d。服务端 `ValidateDashboard` 用同一套语法做权威校验。
 */
const tokenPattern = /^now(-[0-9]{1,4}(s|m|h|d))?$/
const shiftPattern = /^[0-9]{1,4}(s|m|h|d)$/
const unitSeconds: Record<string, number> = { s: 1, m: 60, h: 3600, d: 86400 }
export const minimumOverrideSeconds = 60
export const maximumOverrideSeconds = 31 * 24 * 60 * 60

/** 把 `now` / `now-6h` 解析为相对现在的秒数；不合法返回 null。 */
export function panelTimeTokenSeconds(token: string): number | null {
  const match = tokenPattern.exec(token)
  if (!match) return null
  const suffix = match[1]
  if (!suffix) return 0
  const unit = suffix.at(-1) ?? ""
  const seconds = unitSeconds[unit]
  if (!seconds) return null
  return Number(suffix.slice(1, -1)) * seconds
}

/** 覆盖配置有误时返回文案键，调用方据此提示并阻止保存。 */
export function panelTimeOverrideError(panel: DashboardPanel): string | null {
  const from = panel.time_from ?? "", to = panel.time_to ?? ""
  if (!from && !to) return null
  if (!from) return "workspace.time_override_invalid"
  const start = panelTimeTokenSeconds(from)
  const end = to === "" ? 0 : panelTimeTokenSeconds(to)
  if (start === null || end === null) return "workspace.time_override_invalid"
  const window = start - end
  if (window < minimumOverrideSeconds || window > maximumOverrideSeconds) return "workspace.time_override_invalid"
  return null
}

/** 覆盖合法时给出该面板自己的查询窗口；留空或非法时返回 null，调用方沿用看板范围。 */
export function panelTimeDomain(panel: DashboardPanel, now = Date.now()): { from: string; to: string } | null {
  const from = panel.time_from ?? ""
  if (!from || panelTimeOverrideError(panel)) return null
  const start = panelTimeTokenSeconds(from) ?? 0
  const end = panel.time_to ? panelTimeTokenSeconds(panel.time_to) ?? 0 : 0
  return { from: new Date(now - start * 1000).toISOString(), to: new Date(now - end * 1000).toISOString() }
}
/** 时间平移：Grafana 风格的时长（`1d`、`12h`）；留空表示不平移，非法返回 null。 */
export function panelTimeShiftSeconds(panel: DashboardPanel): number | null {
  const token = panel.time_shift ?? ""
  if (!shiftPattern.test(token)) return null
  const seconds = panelTimeTokenSeconds(`now-${token}`)
  return seconds !== null && seconds >= 1 && seconds <= maximumOverrideSeconds ? seconds : null
}

/** 时间平移非法时返回文案键，调用方据此提示并阻止保存。 */
export function panelTimeShiftError(panel: DashboardPanel): string | null {
  if (!panel.time_shift) return null
  return panelTimeShiftSeconds(panel) === null ? "workspace.time_shift_invalid" : null
}

/** 把窗口整体后移；用于「对比上一周期」。 */
export function shiftQueryDomain(domain: { from: string; to: string }, seconds: number): { from: string; to: string } {
  const shift = seconds * 1000
  return { from: new Date(Date.parse(domain.from) - shift).toISOString(), to: new Date(Date.parse(domain.to) - shift).toISOString() }
}

/** 面板生效查询窗口：先应用面板时间覆盖，再按时间平移整体后移。 */
export function panelQueryDomain(panel: DashboardPanel, domain: { from: string; to: string }, now = Date.now()): { from: string; to: string } {
  const base = panelTimeDomain(panel, now) ?? domain
  const shift = panelTimeShiftSeconds(panel)
  return shift === null ? base : shiftQueryDomain(base, shift)
}

/** 面板头徽标：显示时间覆盖与平移的 token，跟 Grafana 的面板时间选项一致。 */
export function panelTimeLabel(panel: DashboardPanel): string | null {
  const override = panel.time_from ? (panel.time_to ? `${panel.time_from} → ${panel.time_to}` : panel.time_from) : ""
  const shift = panel.time_shift ? `↩ ${panel.time_shift}` : ""
  return [override, shift].filter(Boolean).join(" · ") || null
}
