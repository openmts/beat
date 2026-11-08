import { append, element } from "./dom.js"
import { decimal, formatDate, shortDate } from "./format.js"
import { strings } from "./i18n.js"
import { state } from "./state.js"
import { detailHeading, detailMeta } from "./components.js"
import { mount, pageStatus } from "./shell.js"
import { chartBlock, networkChartBlock, registerCharts } from "./charts.js"
import { runtime } from "./runtime.js"

const rangeLabels = new Map([[300, "lastFiveMinutes"], [900, "lastFifteenMinutes"], [3600, "lastHour"], [21600, "lastSixHours"],
  [86400, "lastDay"], [604800, "lastSevenDays"], [2592000, "lastThirtyDays"]])

function tabButton(id, label, selected) {
  const button = element("button", "tab", label)
  button.type = "button"
  button.setAttribute("role", "tab")
  button.setAttribute("aria-selected", String(selected))
  button.setAttribute("aria-controls", `panel-${id}`)
  button.id = `tab-${id}`
  button.tabIndex = selected ? 0 : -1
  button.addEventListener("click", () => {
    state.tab = id
    renderDetail(state.snapshot)
  })
  return button
}

function tabs(snapshot, node, locale) {
  const t = strings(locale)
  const selected = state.tab === "network" ? "network" : "resources"
  const list = element("div", "tab-list")
  list.setAttribute("role", "tablist")
  append(list, tabButton("resources", t.resources, selected === "resources"), tabButton("network", t.networkLatency, selected === "network"))
  const panel = element("section", "tab-panel")
  panel.id = `panel-${selected}`
  panel.setAttribute("role", "tabpanel")
  panel.setAttribute("aria-labelledby", `tab-${selected}`)
  panel.append(selected === "network" ? networkPanel(snapshot, node, locale) : resourcesPanel(snapshot, locale))
  const root = element("div", "detail-tabs")
  append(root, list, panel)
  return root
}

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
    if (next < distance) { distance = next; active = seconds }
  }
  return duration ? active : -1
}

function chooseRange(row, seconds) {
  state.pendingRange = seconds
  const chips = row.querySelectorAll(".range-chip")
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
  const configured = Array.isArray(snapshot.range_options) ? snapshot.range_options.filter((seconds) => rangeLabels.has(seconds)) : []
  const ranges = configured.length ? configured : [3600, 21600, 86400, 604800]
  const active = activeRange(snapshot)
  const row = element("div", "range-row")
  row.setAttribute("aria-label", t.currentRange)
  for (const seconds of ranges) {
    const chip = element("button", "range-chip", t[rangeLabels.get(seconds)])
    chip.type = "button"
    chip.dataset.seconds = String(seconds)
    chip.setAttribute("aria-pressed", String(seconds === active))
    if (seconds === active) chip.dataset.active = "true"
    if (state.pendingRange === seconds) {
      chip.disabled = true
      chip.dataset.pending = "true"
      chip.setAttribute("aria-busy", "true")
    }
    chip.addEventListener("click", () => chooseRange(row, seconds))
    row.append(chip)
  }
  const bounds = snapshot.history ?? snapshot.network_history
  if (bounds) {
    const from = formatDate(bounds.from * 1000, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }, locale)
    const to = formatDate(bounds.to * 1000, { hour: "2-digit", minute: "2-digit" }, locale)
    row.append(element("span", "range-note", `${from} – ${to}`))
  }
  return row
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
  if (specs.length) return specs
  return Object.keys(series).slice(0, 8).map((key) => ({
    title: key.startsWith("custom_") ? key.slice(7) : key,
    keys: [key],
    unit: info[key]?.unit || "",
  }))
}

function resourcesPanel(snapshot, locale) {
  const t = strings(locale)
  const history = snapshot.history
  const panel = element("div")
  panel.append(rangeRow(snapshot, locale))
  const series = history?.series ?? {}
  if (!Object.keys(series).length) {
    panel.append(element("p", "empty-state", t.noHistory))
    return panel
  }
  const stack = element("div", "chart-stack")
  for (const spec of standardSpecs(snapshot, locale)) stack.append(chartBlock(snapshot, spec, locale))
  panel.append(stack)
  return panel
}

function networkPanel(snapshot, node, locale) {
  const t = strings(locale)
  if (!snapshot.network?.visible) return element("p", "empty-state", t.networkDisabled)
  const tasks = (snapshot.network.tasks ?? []).filter((task) => Array.isArray(task.nodes) && task.nodes.some((entry) => entry.node_id === node.id))
  if (!tasks.length) return element("p", "empty-state", t.noNetwork)
  const panel = element("div")
  panel.append(rangeRow(snapshot, locale))
  const list = element("div", "network-list")
  const bounds = snapshot.network_history ?? snapshot.history
  for (const task of tasks) {
    const result = task.nodes.find((entry) => entry.node_id === node.id)?.latest
    const card = element("article", "network-card")
    const head = element("div", "network-head")
    append(head, element("h2", "network-name", task.name || task.id), element("span", "network-type", task.type || ""))
    let reading = "—"
    let status = t.pending
    if (result) {
      if (result.success && Number.isFinite(result.latency_ms)) {
        reading = `${decimal(result.latency_ms, 1)} ms`
        status = t.success
      } else {
        status = result.status_code ? `HTTP ${result.status_code}` : t.failed
      }
    }
    const time = result?.timestamp ? shortDate(result.timestamp, locale) : t.pending
    append(card, head, element("p", "network-time", `${status}${result ? ` · ${reading}` : ""} · ${time}`))
    const history = snapshot.network_history?.tasks?.find((entry) => entry.id === task.id)
    const now = Math.floor(Date.now() / 1000)
    card.append(networkChartBlock(task.name || task.id, history?.points ?? [], bounds ?? { from: now - 3600, to: now }, locale))
    list.append(card)
  }
  panel.append(list)
  return panel
}

function detail(snapshot, locale) {
  const node = snapshot.nodes.find((candidate) => candidate.id === snapshot.route?.node_id) ?? snapshot.nodes[0]
  const main = element("main", "page detail-page")
  if (!node) {
    main.append(element("p", "empty-state", pageStatus(snapshot, locale)?.text ?? strings(locale).empty))
  } else {
    append(main, detailHeading(node, locale), detailMeta(snapshot, node, locale), tabs(snapshot, node, locale))
  }
  mount(snapshot, main, locale)
}

export function renderDetail(snapshot, locale) {
  detail(snapshot, locale)
  registerCharts()
}
