import { append, element } from "./dom.js"
import { decimal, formatDuration, formatRate, percentValue, shortDate } from "./format.js"
import { strings } from "./i18n.js"
import { runtime } from "./runtime.js"
import { state } from "./state.js"
import { chartBlock, networkChartBlock } from "./charts.js"
import { countryBadge, diskDetail, groupName, loadMetric, memoryDetail, nodeName, status } from "./paper-components.js"

const rangeLabels = new Map([[300, "lastFiveMinutes"], [900, "lastFifteenMinutes"], [3600, "lastHour"], [21600, "lastSixHours"],
  [86400, "lastDay"], [604800, "lastSevenDays"], [2592000, "lastThirtyDays"]])

function historyDuration(history) {
  if (!history || !Number.isFinite(history.from) || !Number.isFinite(history.to) || history.to <= history.from) return 0
  return history.to - history.from
}

function activeRange(snapshot) {
  if (snapshot.range?.mode === "relative" && Number.isFinite(snapshot.range.seconds)) return snapshot.range.seconds
  if (snapshot.range?.mode === "absolute") return -1
  const duration = historyDuration(snapshot.history)
  let active = -1
  let distance = Infinity
  for (const seconds of rangeLabels.keys()) {
    const next = Math.abs(duration - seconds)
    if (next < distance) {
      distance = next
      active = seconds
    }
  }
  return duration ? active : -1
}

function chooseRange(row, seconds) {
  state.pendingRange = seconds
  const chips = row.querySelectorAll(".paper-range-chip")
  for (const chip of chips) {
    chip.disabled = true
    if (Number(chip.dataset.seconds) === seconds) {
      chip.dataset.pending = "true"
      chip.setAttribute("aria-busy", "true")
    }
  }
  if (runtime.setRange({ mode: "relative", seconds })) return
  state.pendingRange = null
  for (const chip of chips) {
    chip.disabled = false
    delete chip.dataset.pending
    chip.removeAttribute("aria-busy")
  }
}

function rangeRow(snapshot, locale) {
  const t = strings(locale)
  const available = Array.isArray(snapshot.range_options) ? snapshot.range_options.filter((seconds) => rangeLabels.has(seconds)) : []
  const row = element("div", "paper-range-row")
  row.setAttribute("aria-label", t.currentRange)
  for (const seconds of available.length ? available : [3600, 21600, 86400, 604800]) {
    const chip = element("button", "paper-range-chip", t[rangeLabels.get(seconds)])
    chip.type = "button"
    chip.dataset.seconds = String(seconds)
    if (seconds === activeRange(snapshot)) chip.dataset.active = "true"
    chip.setAttribute("aria-pressed", String(seconds === activeRange(snapshot)))
    if (state.pendingRange === seconds) {
      chip.disabled = true
      chip.dataset.pending = "true"
      chip.setAttribute("aria-busy", "true")
    }
    chip.addEventListener("click", () => chooseRange(row, seconds))
    row.append(chip)
  }
  return row
}

function statCell(label, value, detail, percentage, className = "") {
  const cell = element("div", `paper-stat${className ? ` ${className}` : ""}`)
  append(cell, element("span", "paper-stat-label", label), element("span", "paper-stat-value", value), element("span", "paper-stat-detail", detail))
  if (Number.isFinite(percentage)) {
    const track = element("span", "paper-stat-meter")
    const fill = element("span", "paper-stat-fill")
    fill.style.width = `${Math.max(0, Math.min(100, percentage))}%`
    track.append(fill)
    cell.append(track)
  }
  return cell
}

function stats(node, locale) {
  const t = strings(locale)
  const metrics = node.metrics ?? {}
  const root = element("section", "paper-stats")
  append(root,
    statCell(t.cpu, percentValue(metrics.cpu), `${t.load} ${loadMetric(metrics)}`, metrics.cpu),
    statCell(t.memory, percentValue(metrics.memory), memoryDetail(metrics), metrics.memory),
    statCell(t.disk, percentValue(metrics.disk), diskDetail(metrics), metrics.disk),
    statCell(t.networkRate, `${formatRate(metrics.net_recv)} ↓`, `${formatRate(metrics.net_sent)} ↑`, NaN, "paper-network-stat"),
  )
  return root
}

function heading(snapshot, node, locale) {
  const t = strings(locale)
  const root = element("header", "paper-detail-head")
  const titleLine = element("div", "paper-detail-title-line")
  append(titleLine, countryBadge(node), element("h2", "paper-detail-title", nodeName(node)), status(node, locale))
  const meta = [node.country_code, groupName(snapshot, node), node.host].filter(Boolean).join(" · ")
  append(root, titleLine, element("p", "paper-detail-meta", meta || t.unknown))
  return root
}

function tabButton(id, label, selected, rerender) {
  const button = element("button", "paper-tab", label)
  button.type = "button"
  button.setAttribute("role", "tab")
  button.setAttribute("aria-selected", String(selected))
  button.addEventListener("click", () => {
    state.tab = id
    rerender()
  })
  return button
}

function standardSpecs(snapshot, locale) {
  const t = strings(locale)
  const series = snapshot.history?.series ?? {}
  const info = snapshot.history?.series_info ?? {}
  const specs = []
  if (series.cpu) specs.push({ title: t.cpu, keys: ["cpu"], unit: info.cpu?.unit || "percent" })
  if (series.memory) specs.push({ title: t.memory, keys: ["memory"], unit: info.memory?.unit || "percent" })
  const networkKeys = ["net_recv", "net_sent"].filter((key) => series[key])
  if (networkKeys.length) specs.push({ title: t.networkRate, keys: networkKeys, labels: [t.download, t.upload], unit: "bytes/s" })
  if (series.disk) specs.push({ title: t.disk, keys: ["disk"], unit: info.disk?.unit || "percent" })
  return specs.length ? specs : Object.keys(series).slice(0, 8).map((key) => ({
    title: key.startsWith("custom_") ? key.slice(7) : key, keys: [key], unit: info[key]?.unit || "",
  }))
}

function resourcesPanel(snapshot, locale) {
  const panel = element("div", "paper-panel")
  const series = snapshot.history?.series ?? {}
  if (!Object.keys(series).length) {
    panel.append(element("p", "paper-empty", strings(locale).noHistory))
    return panel
  }
  const grid = element("div", "paper-chart-grid")
  for (const spec of standardSpecs(snapshot, locale)) grid.append(chartBlock(snapshot, spec, locale))
  panel.append(grid)
  return panel
}

function networkPanel(snapshot, node, locale) {
  const t = strings(locale)
  if (!snapshot.network?.visible) return element("p", "paper-empty", t.networkDisabled)
  const tasks = (snapshot.network.tasks ?? []).filter((task) => Array.isArray(task.nodes) && task.nodes.some((entry) => entry.node_id === node.id))
  if (!tasks.length) return element("p", "paper-empty", t.noNetwork)
  const panel = element("div", "paper-panel")
  const grid = element("div", "paper-network-grid")
  const bounds = snapshot.network_history ?? snapshot.history
  for (const task of tasks) {
    const latest = task.nodes.find((entry) => entry.node_id === node.id)?.latest
    const history = snapshot.network_history?.tasks?.find((entry) => entry.id === task.id)
    const card = element("article", "paper-network-card")
    const head = element("div", "paper-network-head")
    append(head, element("h3", "paper-network-name", task.name || task.id), element("span", "paper-network-type", task.type || ""))
    const reading = latest?.success && Number.isFinite(latest.latency_ms) ? `${decimal(latest.latency_ms, 1)} ms` : "—"
    const when = latest?.timestamp ? shortDate(latest.timestamp, locale) : t.pending
    const series = history?.series?.length ? history.series : history?.points ?? []
    append(card, head, element("p", "paper-network-reading", `${reading} · ${when}`), networkChartBlock(task.name || task.id, series, bounds, locale))
    grid.append(card)
  }
  panel.append(grid)
  return panel
}

function tabs(snapshot, node, locale, rerender) {
  const t = strings(locale)
  const selected = state.tab === "network" ? "network" : "resources"
  const list = element("div", "paper-tabs")
  list.setAttribute("role", "tablist")
  append(list, tabButton("resources", t.resources, selected === "resources", rerender), tabButton("network", t.networkLatency, selected === "network", rerender))
  const panel = element("section", "paper-tab-panel")
  panel.append(selected === "network" ? networkPanel(snapshot, node, locale) : resourcesPanel(snapshot, locale))
  const root = element("div", "paper-monitoring")
  append(root, list, rangeRow(snapshot, locale), panel)
  return root
}

function infoItem(label, value) {
  const item = element("div", "paper-info-item")
  append(item, element("dt", "", label), element("dd", "", value || "—"))
  return item
}

function systemInfo(snapshot, node, locale) {
  const t = strings(locale)
  const system = node.system ?? {}
  const metrics = node.metrics ?? {}
  const root = element("section", "paper-system")
  root.append(element("h3", "paper-section-title", t.systemInfo))
  const grid = element("dl", "paper-info-grid")
  append(grid,
    infoItem(t.system, [system.os, system.os_version].filter(Boolean).join(" ")),
    infoItem(t.kernel, system.kernel),
    infoItem(t.architecture, [system.arch, system.virtualization].filter(Boolean).join(" · ")),
    infoItem(t.processor, system.cpu_model),
    infoItem(t.uptime, formatDuration(metrics.uptime, locale)),
    infoItem(t.lastSeen, node.last_seen ? shortDate(node.last_seen, locale) : ""),
  )
  root.append(grid)
  return root
}

export function workspace(snapshot, node, locale, rerender) {
  if (!node) {
    const empty = element("main", "paper-workspace paper-workspace-empty")
    empty.append(element("p", "paper-empty", snapshot.nodes.length ? strings(locale).openingNode : strings(locale).selectPrompt))
    return empty
  }
  const root = element("main", "paper-workspace")
  const content = element("div", "paper-workspace-inner")
  const groups = element("div", "paper-detail-groups")
  append(groups, stats(node, locale), tabs(snapshot, node, locale, rerender), systemInfo(snapshot, node, locale))
  append(content, heading(snapshot, node, locale), groups)
  root.append(content)
  return root
}
