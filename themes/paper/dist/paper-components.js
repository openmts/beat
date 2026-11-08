import { append, element } from "./dom.js"
import { clampPercent, decimal, formatBytes, percentValue } from "./format.js"
import { strings } from "./i18n.js"

export function nodeName(node) {
  return node.alias || node.name || node.id
}

export function countryBadge(node) {
  if (typeof node.country_code !== "string" || !/^[A-Z]{2}$/.test(node.country_code)) return null
  const badge = element("span", "paper-country", node.country_code)
  badge.title = node.country_code
  badge.setAttribute("aria-label", `Country ${node.country_code}`)
  return badge
}

export function status(node, locale) {
  const t = strings(locale)
  const root = element("span", "paper-status")
  root.dataset.status = node.status === "online" ? "online" : "offline"
  append(root, element("span", "paper-status-dot"), element("span", "", node.status === "online" ? t.online : t.offline))
  return root
}

export function systemLabel(node) {
  const system = node.system ?? {}
  return [system.os, system.os_version].filter(Boolean).join(" ") || system.platform || "—"
}

export function groupName(snapshot, node) {
  return snapshot.groups?.find((group) => group.id === node.group_id)?.name ?? ""
}

export function memoryDetail(metrics) {
  return `${formatBytes(metrics.memory_used)} / ${formatBytes(metrics.memory_total)}`
}

export function diskDetail(metrics) {
  return `${formatBytes(metrics.disk_used)} / ${formatBytes(metrics.disk_total)}`
}

export function trafficPercentage(traffic) {
  if (Number.isFinite(traffic?.percentage)) return traffic.percentage
  if (Number.isFinite(traffic?.used) && Number.isFinite(traffic?.limit) && traffic.limit > 0) {
    return traffic.used / traffic.limit * 100
  }
  return NaN
}

export function trafficLabel(traffic, locale) {
  if (!Number.isFinite(traffic?.used)) return strings(locale).unknown
  const limit = traffic.limit === null ? strings(locale).unlimited : Number.isFinite(traffic.limit) ? formatBytes(traffic.limit) : strings(locale).unknown
  return `${formatBytes(traffic.used)} / ${limit}`
}

export function meter(value, className = "paper-meter") {
  const track = element("span", className)
  const fill = element("span", "paper-meter-fill")
  fill.style.width = `${clampPercent(value)}%`
  track.append(fill)
  return track
}

export function percentMetric(value) {
  return percentValue(value)
}

export function loadMetric(metrics) {
  if (!Number.isFinite(metrics.load1)) return "—"
  return [metrics.load1, metrics.load5, metrics.load15].filter(Number.isFinite).map((value) => decimal(value, 2)).join(" / ")
}

export function matchesQuery(node, query) {
  const needle = query.trim().toLocaleLowerCase()
  if (!needle) return true
  return [nodeName(node), node.country_code, node.group_id, node.status, systemLabel(node), node.host]
    .filter(Boolean)
    .some((value) => String(value).toLocaleLowerCase().includes(needle))
}
