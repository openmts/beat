// Pulse 示例脚本主题：只消费 SDK 暴露的公开快照，全部数据以 textContent 写入。
import { connectBeatTheme } from "./beat-sdk.js"

const copy = {
  en: {
    online: "Online", offline: "Offline", updated: "Updated", nodes: (count) => `${count} public nodes`,
    empty: "No public nodes are visible yet.", loading: "Loading public data…",
    error: "Public data is temporarily unavailable.", cpu: "CPU", memory: "Memory", disk: "Disk",
    open: (name) => `Open ${name}`, freshness: { fresh: "live", delayed: "delayed", stale: "stale", unavailable: "no data" },
  },
  "zh-CN": {
    online: "在线", offline: "离线", updated: "更新于", nodes: (count) => `${count} 个公开节点`,
    empty: "当前没有可见的公开节点。", loading: "正在加载公开数据…",
    error: "公开数据暂时不可用。", cpu: "CPU", memory: "内存", disk: "磁盘",
    open: (name) => `查看 ${name}`, freshness: { fresh: "实时", delayed: "延迟", stale: "陈旧", unavailable: "无数据" },
  },
}

const maxPercent = 100
const warnPercent = 75
const badPercent = 90
const element = (id) => document.getElementById(id)
const text = (id, value) => { const node = element(id); if (node) node.textContent = value }
const strings = () => copy[state.locale] ?? copy.en

function level(value) {
  if (!Number.isFinite(value)) return "none"
  if (value >= badPercent) return "bad"
  return value >= warnPercent ? "warn" : "ok"
}

function percentText(value) {
  return Number.isFinite(value) ? `${Math.round(value)}%` : "—"
}

function meter(label, value) {
  const wrap = document.createElement("div")
  wrap.className = "pulse-meter"
  const head = document.createElement("div")
  head.className = "pulse-meter-label"
  const name = document.createElement("span")
  name.textContent = label
  const reading = document.createElement("span")
  reading.className = "pulse-meter-value"
  reading.textContent = percentText(value)
  const track = document.createElement("div")
  track.className = "pulse-track"
  const fill = document.createElement("div")
  fill.className = "pulse-fill"
  fill.dataset.level = level(value)
  fill.style.width = `${Math.max(0, Math.min(maxPercent, Number.isFinite(value) ? value : 0))}%`
  head.append(name, reading)
  track.append(fill)
  wrap.append(head, track)
  return wrap
}

function card(node, client) {
  const localized = strings()
  const displayName = node.alias || node.name || node.id
  const article = document.createElement("article")
  article.className = "pulse-card"
  const head = document.createElement("div")
  head.className = "pulse-card-head"
  const open = document.createElement("button")
  open.type = "button"
  open.className = "pulse-open"
  open.textContent = displayName
  open.setAttribute("aria-label", localized.open(displayName))
  open.addEventListener("click", () => { client.navigate(`/node/${node.id}`) })
  const pill = document.createElement("span")
  pill.className = "pulse-pill"
  pill.dataset.status = node.status === "online" ? "online" : "offline"
  pill.textContent = node.status === "online" ? localized.online : localized.offline
  head.append(open, pill)
  article.append(head)
  if (localized.freshness[node.telemetry_state]) {
    const freshness = document.createElement("span")
    freshness.className = "pulse-pill"
    freshness.dataset.freshness = node.telemetry_state
    freshness.textContent = localized.freshness[node.telemetry_state]
    article.append(freshness)
  }
  const metrics = node.metrics ?? {}
  const meters = document.createElement("div")
  meters.className = "pulse-meters"
  meters.append(meter(localized.cpu, metrics.cpu), meter(localized.memory, metrics.memory), meter(localized.disk, metrics.disk))
  article.append(meters)
  if (node.tags?.length) {
    const tags = document.createElement("ul")
    tags.className = "pulse-tags"
    for (const tag of node.tags) {
      const item = document.createElement("li")
      item.textContent = tag
      tags.append(item)
    }
    article.append(tags)
  }
  if (node.public_remark) {
    const remark = document.createElement("p")
    remark.className = "pulse-remark"
    remark.textContent = node.public_remark
    article.append(remark)
  }
  return article
}

function statusMessage(snapshot) {
  const localized = strings()
  const metrics = snapshot.state?.metrics
  const nodes = snapshot.state?.nodes
  if (nodes === "error" || metrics === "error") return { state: "error", message: localized.error }
  if (snapshot.nodes.length === 0 && (nodes === "loading" || metrics === "loading")) return { state: "loading", message: localized.loading }
  return { state: "ready", message: "" }
}

function renderSnapshot(snapshot, client) {
  const localized = strings()
  text("site-title", snapshot.site?.site_title || "Beat")
  text("site-description", snapshot.site?.site_description || "")
  text("online-label", localized.online)
  text("offline-label", localized.offline)
  text("updated-label", localized.updated)
  const online = snapshot.nodes.filter((node) => node.status === "online").length
  text("online-count", String(online))
  text("offline-count", String(snapshot.nodes.length - online))
  text("updated-at", snapshot.timestamp ? new Date(snapshot.timestamp).toLocaleTimeString() : "—")
  text("node-count", localized.nodes(snapshot.nodes.length))
  text("locale", snapshot.locale ?? "")
  const status = statusMessage(snapshot)
  const banner = element("status")
  if (banner) {
    banner.textContent = status.message
    banner.dataset.state = status.state
  }
  const grid = element("node-grid")
  if (!grid) return
  grid.replaceChildren()
  if (snapshot.nodes.length === 0) {
    const empty = document.createElement("p")
    empty.className = "pulse-empty"
    empty.textContent = status.state === "ready" ? localized.empty : status.message
    grid.append(empty)
    return
  }
  for (const node of snapshot.nodes) grid.append(card(node, client))
}

const state = { locale: "en" }

const client = connectBeatTheme((snapshot) => {
  state.locale = snapshot.locale ?? state.locale
  document.documentElement.lang = state.locale
  renderSnapshot(snapshot, client)
})

addEventListener("pagehide", () => client.destroy())
