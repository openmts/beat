import { append, element } from "./dom.js"
import { decimal, formatBytes, formatDuration, shortDate } from "./format.js"
import { strings } from "./i18n.js"

export function countryNode(node) {
  if (typeof node.country_code !== "string" || !/^[A-Z]{2}$/.test(node.country_code)) return null
  const badge = element("span", "country-flag", node.country_code)
  badge.title = node.country_code
  badge.setAttribute("aria-label", `Country ${node.country_code}`)
  return badge
}

export function nodeName(node) {
  return node.alias || node.name || node.id
}

export function groupName(snapshot, node) {
  return snapshot.groups?.find((group) => group.id === node.group_id)?.name ?? ""
}

export function systemSummary(node) {
  const system = node.system ?? {}
  return [system.virtualization, system.arch, system.platform].filter(Boolean).join(" · ")
}

export function statusPill(node, locale) {
  const t = strings(locale)
  const online = node.status === "online"
  const pill = element("span", "status-pill")
  pill.dataset.status = online ? "online" : "offline"
  const duration = online ? formatDuration(node.metrics?.uptime, locale) : shortDate(node.last_seen, locale)
  append(pill, element("span", "status-dot"), element("span", "", duration ? `${online ? t.online : t.offline} ${duration}` : online ? t.online : t.offline))
  return pill
}

export function usedTotal(used, total, locale) {
  if (!Number.isFinite(used) && !Number.isFinite(total)) return strings(locale).unknown
  return `${Number.isFinite(used) ? formatBytes(used) : "—"} / ${Number.isFinite(total) ? formatBytes(total) : "—"}`
}

export function trafficPercentage(traffic) {
  if (Number.isFinite(traffic?.percentage)) return traffic.percentage
  if (Number.isFinite(traffic?.used) && Number.isFinite(traffic?.limit) && traffic.limit > 0) return traffic.used / traffic.limit * 100
  return NaN
}

export function trafficDetail(traffic, locale) {
  const t = strings(locale)
  if (!Number.isFinite(traffic?.used)) return t.unknown
  const limit = traffic.limit === null ? t.unlimited : Number.isFinite(traffic.limit) ? formatBytes(traffic.limit) : t.unknown
  return `${formatBytes(traffic.used)} / ${limit}`
}

export function metaItem(label, value, sub) {
  const item = element("div")
  append(item, element("p", "meta-label", label), element("p", "meta-value", value))
  if (sub) item.append(element("p", "meta-sub", sub))
  return item
}

function processLabel(system, locale) {
  const count = system?.processes
  return Number.isFinite(count) ? `${count} ${strings(locale).processes}` : ""
}

export function detailMeta(snapshot, node, locale) {
  const t = strings(locale)
  const metrics = node.metrics ?? {}
  const system = node.system ?? {}
  const traffic = node.traffic ?? {}
  const os = [system.os, system.os_version].filter(Boolean).join(" ")
  const architecture = [system.arch, system.virtualization, processLabel(node.metrics, locale)].filter(Boolean).join(" · ")
  const trafficValue = `↓ ${formatBytes(metrics.net_recv_total)} · ↑ ${formatBytes(metrics.net_sent_total)}`
  const grid = element("section", "detail-meta")
  append(grid,
    metaItem(t.system, os || t.unknown, system.kernel),
    metaItem(t.cpu, system.cpu_model || t.unknown, Number.isFinite(metrics.cpu_total) ? t.coreCount(metrics.cpu_total) : ""),
    metaItem(`${t.memory} / ${t.disk}`, usedTotal(metrics.memory_used, metrics.memory_total, locale), usedTotal(metrics.disk_used, metrics.disk_total, locale)),
    metaItem(t.architecture, architecture || system.platform || t.unknown),
    metaItem(t.traffic, trafficValue, trafficDetail(traffic, locale)),
    metaItem(t.lastSeen, shortDate(node.last_seen, locale), groupName(snapshot, node) || node.host || ""))
  return grid
}

export function detailHeading(node, locale) {
  const t = strings(locale)
  const heading = element("div", "detail-heading")
  const title = element("div", "detail-title")
  append(title, countryNode(node), element("h1", "", nodeName(node)))
  const badges = element("div", "badges")
  badges.append(statusPill(node, locale))
  append(heading, title, badges)
  return heading
}

export function loadLine(metrics, locale) {
  const t = strings(locale)
  return Number.isFinite(metrics.load1)
    ? `${t.load} ${decimal(metrics.load1, 2)} ${decimal(metrics.load5, 2)} ${decimal(metrics.load15, 2)}`
    : t.unknown
}
