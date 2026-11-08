import { append, element } from "./dom.js"
import { formatDate } from "./format.js"
import { strings } from "./i18n.js"
import { runtime } from "./runtime.js"

function topbar(snapshot, detail, locale) {
  const t = strings(locale)
  const bar = element("header", "topbar")
  const inner = element("div", "topbar-inner")
  const brand = element(detail ? "button" : "div", `brand${detail ? " is-link" : ""}`, snapshot.site?.site_title || "Beat")
  if (detail) {
    brand.type = "button"
    brand.setAttribute("aria-label", t.back)
    brand.addEventListener("click", () => runtime.navigate("/"))
  }
  const meta = element("div", "top-meta")
  const live = element("span", "live-mark")
  append(live, element("span", "live-dot"), element("span", "", t.live))
  const online = snapshot.nodes.filter((node) => node.status === "online").length
  append(meta, element("span", "", t.onlineCount(online, snapshot.nodes.length)), live)
  if (snapshot.timestamp) meta.title = `${t.updated} ${formatDate(snapshot.timestamp, { hour: "2-digit", minute: "2-digit", second: "2-digit" }, locale)}`
  append(inner, brand, meta)
  bar.append(inner)
  return bar
}

export function pageStatus(snapshot, locale) {
  const t = strings(locale)
  if (snapshot.state?.nodes === "error" || snapshot.state?.metrics === "error") return { state: "error", text: t.error }
  if (!snapshot.nodes.length && snapshot.state?.nodes === "loading") return { state: "loading", text: t.loading }
  if (!snapshot.nodes.length) return { state: "ready", text: t.empty }
  return null
}

export function mount(snapshot, main, locale) {
  const app = document.getElementById("app")
  if (!app) return
  document.documentElement.dataset.route = snapshot.route?.kind === "node" ? "detail" : "dashboard"
  const title = snapshot.route?.kind === "node" ? snapshot.nodes[0]?.alias || snapshot.nodes[0]?.name : snapshot.site?.site_title
  document.title = [title, "Beat Monitor"].filter(Boolean).join(" · ")
  app.replaceChildren(topbar(snapshot, snapshot.route?.kind === "node", locale), main)
}
