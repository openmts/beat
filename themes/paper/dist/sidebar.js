import { append, element } from "./dom.js"
import { formatBytes, percentValue } from "./format.js"
import { strings } from "./i18n.js"
import { runtime } from "./runtime.js"
import { state } from "./state.js"
import { countryBadge, matchesQuery, meter, nodeName, status, systemLabel, trafficLabel, trafficPercentage } from "./paper-components.js"

function groupTabs(snapshot, locale, rerender) {
  const t = strings(locale)
  const root = element("div", "paper-groups")
  root.setAttribute("role", "tablist")
  const entries = [{ id: "all", name: t.allGroups, count: snapshot.nodes.length }]
  for (const group of snapshot.groups ?? []) {
    const count = snapshot.nodes.filter((node) => node.group_id === group.id).length
    if (count) entries.push({ id: group.id, name: group.name, count })
  }
  for (const entry of entries) {
    const button = element("button", "paper-group", `${entry.name} ${entry.count}`)
    button.type = "button"
    button.setAttribute("role", "tab")
    button.setAttribute("aria-selected", String(state.group === entry.id))
    if (state.group === entry.id) button.dataset.active = "true"
    button.addEventListener("click", () => {
      state.group = entry.id
      rerender()
    })
    root.append(button)
  }
  return root
}

function miniMetric(label, value, detail, percentage) {
  const item = element("div", "paper-mini-metric")
  const head = element("div", "paper-mini-head")
  append(head, element("span", "", label), element("span", "paper-mini-value", value))
  append(item, head, meter(percentage, "paper-mini-meter"), element("span", "paper-mini-detail", detail))
  return item
}

function nodeRow(snapshot, node, locale, selected) {
  const t = strings(locale)
  const metrics = node.metrics ?? {}
  const traffic = node.traffic ?? {}
  const button = element("button", "paper-node")
  button.type = "button"
  button.dataset.selected = String(selected)
  button.setAttribute("aria-current", selected ? "page" : "false")
  button.setAttribute("aria-label", `${t.openNode} ${nodeName(node)}`)
  button.addEventListener("click", () => runtime.navigate(`/node/${node.id}`))

  const top = element("div", "paper-node-top")
  const identity = element("div", "paper-node-identity")
  append(identity, countryBadge(node), element("span", "paper-node-name", nodeName(node)))
  append(top, identity, status(node, locale))

  const meta = element("div", "paper-node-meta")
  const group = snapshot.groups?.find((candidate) => candidate.id === node.group_id)?.name ?? ""
  append(meta, element("span", "paper-node-system", systemLabel(node)), group ? element("span", "paper-node-group", group) : null)

  const mini = element("div", "paper-mini-grid")
  append(mini,
    miniMetric(t.cpu, percentValue(metrics.cpu), Number.isFinite(metrics.cpu_total) ? `${metrics.cpu_total} CPU` : "", metrics.cpu),
    miniMetric(t.memory, percentValue(metrics.memory), `${formatBytes(metrics.memory_used)} / ${formatBytes(metrics.memory_total)}`, metrics.memory),
    miniMetric(t.disk, percentValue(metrics.disk), `${formatBytes(metrics.disk_used)} / ${formatBytes(metrics.disk_total)}`, metrics.disk),
  )

  const trafficRow = element("div", "paper-node-traffic")
  append(trafficRow,
    element("span", "", t.traffic),
    meter(trafficPercentage(traffic), "paper-traffic-meter"),
    element("span", "paper-traffic-value", trafficLabel(traffic, locale)),
  )
  append(button, top, meta, mini, trafficRow)
  return button
}

function filteredNodes(snapshot) {
  return snapshot.nodes.filter((node) => (state.group === "all" || node.group_id === state.group) && matchesQuery(node, state.query))
}

function renderList(list, snapshot, locale, selectedId) {
  list.replaceChildren()
  const nodes = filteredNodes(snapshot)
  if (!nodes.length) {
    list.append(element("p", "paper-node-empty", strings(locale).noResults))
    return
  }
  for (const node of nodes) {
    list.append(nodeRow(snapshot, node, locale, node.id === selectedId))
  }
}

function searchBox(snapshot, locale, list, selectedId) {
  const input = element("input", "paper-search-input")
  input.type = "search"
  input.value = state.query
  input.placeholder = strings(locale).searchPlaceholder
  input.setAttribute("aria-label", strings(locale).searchPlaceholder)
  input.addEventListener("input", () => {
    state.query = input.value
    renderList(list, snapshot, locale, selectedId)
  })
  const field = element("label", "paper-search")
  append(field, element("span", "paper-search-icon", "⌕"), input)
  return field
}

export function sidebar(snapshot, locale, selectedId, rerender) {
  const root = element("aside", "paper-sidebar")
  const tabs = groupTabs(snapshot, locale, rerender)
  const list = element("div", "paper-node-list")
  const toolbar = element("div", "paper-sidebar-toolbar")
  append(toolbar, tabs, element("div", "paper-search-row"))
  const searchRow = toolbar.querySelector(".paper-search-row")
  append(searchRow, searchBox(snapshot, locale, list, selectedId), element("span", "paper-live-label", `● ${strings(locale).live}`))
  renderList(list, snapshot, locale, selectedId)
  append(root, toolbar, list)
  return root
}
