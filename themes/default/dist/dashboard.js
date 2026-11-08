import { append, element } from "./dom.js"
import { clampPercent, formatBytes, formatRate, percentValue } from "./format.js"
import { strings } from "./i18n.js"
import { runtime } from "./runtime.js"
import {
  countryNode,
  groupName,
  loadLine,
  nodeName,
  statusPill,
  systemSummary,
  trafficDetail,
  trafficPercentage,
  usedTotal,
} from "./components.js"
import { mount, pageStatus } from "./shell.js"

function metricBlock(label, value, detail, percentage, extra) {
  const block = element("div", "metric")
  const head = element("div", "metric-head")
  const name = element("span", "metric-label", label)
  if (extra) name.append(element("small", "", extra))
  const reading = element("span", "metric-value", value)
  append(head, name, reading)
  const meter = element("div", "meter")
  const fill = element("div", "meter-fill")
  fill.style.width = `${clampPercent(percentage)}%`
  meter.append(fill)
  append(block, head, meter, element("div", "metric-detail", detail))
  return block
}

function flowItem(arrow, value, label) {
  const item = element("div", "flow-item")
  item.title = `${label}: ${value}`
  const hidden = element("span", "sr-only", label)
  append(item, element("span", "flow-arrow", arrow), hidden, element("span", "flow-value", value))
  return item
}

function flowRow(down, up) {
  const row = element("div", "flow-row")
  append(row, down, up)
  return row
}

function card(snapshot, node, locale) {
  const t = strings(locale)
  const metrics = node.metrics ?? {}
  const traffic = node.traffic ?? {}
  const article = element("article", "node-card")
  article.tabIndex = 0
  article.setAttribute("role", "link")
  article.setAttribute("aria-label", `${t.openNode} ${nodeName(node)}`)
  const open = () => runtime.navigate(`/node/${node.id}`)
  article.addEventListener("click", open)
  article.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" && event.key !== " ") return
    event.preventDefault()
    open()
  })

  const head = element("div", "card-head")
  const line = element("div", "node-line")
  append(line, countryNode(node), element("span", "node-name", nodeName(node)))
  append(head, line, statusPill(node, locale))

  const system = element("div", "system-line")
  append(system, element("span", "", systemSummary(node) || t.unknown), element("span", "", groupName(snapshot, node)))

  const metricGrid = element("div", "metric-grid")
  append(metricGrid,
    metricBlock(t.cpu, percentValue(metrics.cpu), loadLine(metrics, locale), metrics.cpu,
      Number.isFinite(metrics.cpu_total) ? t.coreCount(metrics.cpu_total) : ""),
    metricBlock(t.memory, percentValue(metrics.memory), usedTotal(metrics.memory_used, metrics.memory_total, locale), metrics.memory),
    metricBlock(t.disk, percentValue(metrics.disk), usedTotal(metrics.disk_used, metrics.disk_total, locale), metrics.disk),
    metricBlock(t.traffic, percentValue(trafficPercentage(traffic)), trafficDetail(traffic, locale), trafficPercentage(traffic)))

  const flows = element("div", "flows")
  append(flows,
    flowRow(flowItem("↓", formatRate(metrics.net_recv), t.download), flowItem("↑", formatRate(metrics.net_sent), t.upload)),
    flowRow(flowItem("↓", formatBytes(metrics.net_recv_total), t.download), flowItem("↑", formatBytes(metrics.net_sent_total), t.upload)))

  append(article, head, system, metricGrid, flows)
  return article
}

export function dashboard(snapshot, locale) {
  const main = element("main", "page")
  const status = pageStatus(snapshot, locale)
  if (status) {
    const message = element("p", "status-message", status.text)
    message.dataset.state = status.state
    message.setAttribute("role", "status")
    main.append(message)
  }
  if (!snapshot.nodes.length) {
    main.append(element("p", "empty-state", status?.text ?? strings(locale).empty))
  } else {
    const grid = element("section", "fleet-grid")
    grid.setAttribute("aria-label", strings(locale).nodes)
    for (const node of snapshot.nodes) grid.append(card(snapshot, node, locale))
    main.append(grid)
  }
  mount(snapshot, main, locale)
}
