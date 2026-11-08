import { element } from "./dom.js"
import { registerCharts } from "./charts.js"
import { header } from "./header.js"
import { sidebar } from "./sidebar.js"
import { workspace } from "./workspace.js"
import { runtime } from "./runtime.js"
import { state } from "./state.js"

function appearance(snapshot) {
  return state.appearance ?? (snapshot.theme === "dark" ? "dark" : "light")
}

function toggleAppearance(snapshot, locale) {
  state.appearance = appearance(snapshot) === "dark" ? "light" : "dark"
  renderPaper(snapshot, locale)
}

function selectedNode(snapshot) {
  if (snapshot.route?.kind !== "node") return null
  return snapshot.nodes.find((node) => node.id === snapshot.route.node_id) ?? snapshot.nodes[0] ?? null
}

function navigateToFirst(snapshot) {
  if (snapshot.route?.kind === "node" || !snapshot.nodes.length) return
  const id = snapshot.nodes[0].id
  if (state.autoNavigatedTo === id) return
  if (runtime.navigate(`/node/${id}`)) state.autoNavigatedTo = id
}

function mountShell(snapshot, locale, selected) {
  const app = document.getElementById("app")
  if (!app) return
  const rerender = () => renderPaper(snapshot, locale)
  const shell = element("div", "paper-shell")
  const surface = element("section", "paper-surface")
  const body = element("div", "paper-body")
  const aside = sidebar(snapshot, locale, selected?.id ?? "", rerender)
  const main = workspace(snapshot, selected, locale, rerender)
  body.append(aside, main)
  surface.append(header(snapshot, locale, appearance(snapshot), () => toggleAppearance(snapshot, locale)), body)
  shell.append(surface)
  app.replaceChildren(shell)
}

export function renderPaper(snapshot, locale) {
  state.snapshot = snapshot
  state.locale = locale
  const selected = selectedNode(snapshot)
  const siteTitle = snapshot.site?.site_title || "Beat Monitor"
  document.documentElement.dataset.theme = appearance(snapshot)
  document.documentElement.dataset.route = snapshot.route?.kind === "node" ? "detail" : "dashboard"
  document.title = selected ? [selected.alias || selected.name, siteTitle].filter(Boolean).join(" · ") : siteTitle
  mountShell(snapshot, locale, selected)
  navigateToFirst(snapshot)
  registerCharts()
}
