import { append, element } from "./dom.js"
import { formatBytes, formatDate, formatRate } from "./format.js"
import { strings } from "./i18n.js"

function sum(nodes, pick) {
  return nodes.reduce((total, node) => total + (Number.isFinite(pick(node)) ? pick(node) : 0), 0)
}

function summaryItem(label, value, className = "") {
  const item = element("span", `paper-summary-item${className ? ` ${className}` : ""}`)
  append(item, element("span", "paper-summary-label", label), element("span", "paper-summary-value", value))
  return item
}

function themeButton(locale, appearance, toggle) {
  const t = strings(locale)
  const button = element("button", "paper-theme-toggle", appearance === "dark" ? "☀" : "☾")
  button.type = "button"
  button.title = t.toggleTheme
  button.setAttribute("aria-label", t.toggleTheme)
  button.setAttribute("aria-pressed", String(appearance === "dark"))
  button.addEventListener("click", toggle)
  return button
}

export function header(snapshot, locale, appearance, toggle) {
  const t = strings(locale)
  const online = snapshot.nodes.filter((node) => node.status === "online").length
  const received = sum(snapshot.nodes, (node) => node.metrics?.net_recv)
  const sent = sum(snapshot.nodes, (node) => node.metrics?.net_sent)
  const traffic = sum(snapshot.nodes, (node) => node.traffic?.used)
  const root = element("header", "paper-header")
  const brand = element("div", "paper-brand")
  const title = element("h1", "paper-title", snapshot.site?.site_title || "Beat")
  const subtitle = `${t.fleetOverview} · ${t.updated} ${formatDate(snapshot.timestamp, { hour: "2-digit", minute: "2-digit" }, locale)}`
  append(brand, title, element("p", "paper-subtitle", subtitle))

  const summary = element("div", "paper-summary")
  const onlineItem = summaryItem(t.online, `${online} / ${snapshot.nodes.length}`, "paper-online-summary")
  onlineItem.prepend(element("span", "paper-live-dot"))
  append(summary,
    onlineItem,
    summaryItem(`↓ ${t.download}`, formatRate(received)),
    summaryItem(`↑ ${t.upload}`, formatRate(sent)),
    summaryItem(t.monthlyTraffic, formatBytes(traffic)),
    themeButton(locale, appearance, toggle),
  )
  append(root, brand, summary)
  return root
}
