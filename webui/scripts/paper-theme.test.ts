import { beforeEach, describe, expect, it, vi } from "vitest"
import { renderPaper } from "../../themes/paper/dist/app.js"
import { resetCharts } from "../../themes/paper/dist/charts.js"
import { decimal, formatBytes, formatRate, percentValue } from "../../themes/paper/dist/format.js"
import { strings } from "../../themes/paper/dist/i18n.js"
import { countryBadge, matchesQuery, meter, nodeName, trafficPercentage } from "../../themes/paper/dist/paper-components.js"
import { runtime } from "../../themes/paper/dist/runtime.js"
import { state } from "../../themes/paper/dist/state.js"

describe("paper theme helpers", () => {
  beforeEach(() => { state.locale = "en" })

  it("formats public values and country badges", () => {
    expect(decimal(12.34, 1)).toBe("12.3")
    expect(formatBytes(1024)).toBe("1.00 KB")
    expect(formatRate(2048)).toBe("2.00 KB/s")
    expect(percentValue(41.25)).toBe("41.3%")
    expect(strings("zh-CN").fleetOverview).toBe("节点总览")
    expect(nodeName({ name: "Node", alias: "Tokyo", id: "n" })).toBe("Tokyo")
    expect(countryBadge({ country_code: "JP" })?.textContent).toBe("JP")
    expect(countryBadge({ country_code: "jp" })).toBeNull()
    expect(trafficPercentage({ used: 20, limit: 80 })).toBe(25)
    expect(trafficPercentage({})).toBeNaN()
    expect(matchesQuery({ alias: "Tokyo Edge", country_code: "JP", status: "online", system: { os: "Debian" } }, "debian")).toBe(true)
    expect(matchesQuery({ alias: "Tokyo Edge", country_code: "JP", status: "online", system: {} }, "berlin")).toBe(false)
    expect(meter(55).querySelector<HTMLElement>(".paper-meter-fill")?.style.width).toBe("55%")
  })
})

describe("paper single-page workspace", () => {
  beforeEach(() => {
    state.appearance = null
    state.group = "all"
    state.query = ""
    state.tab = "resources"
    state.pendingRange = null
    state.autoNavigatedTo = ""
    document.body.innerHTML = '<div id="app"></div>'
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null)
    resetCharts()
  })

  it("opens the first node from the dashboard and renders a two-column workspace", () => {
    const navigate = vi.fn(() => true)
    const previous = runtime.navigate
    runtime.navigate = navigate
    try {
      renderPaper(dashboard(), "en")
      expect(navigate).toHaveBeenCalledWith("/node/node-1")
      expect(document.querySelectorAll(".paper-node")).toHaveLength(2)
      expect(document.querySelector(".paper-header")?.textContent).toContain("2 / 2")
      expect(document.querySelector(".paper-workspace-empty")).not.toBeNull()
      expect(document.title).toBe("Beat Monitor")
    } finally { runtime.navigate = previous }
  })

  it("renders detail stats, ranges and switches to network charts", () => {
    const setRange = vi.fn(() => true)
    const previous = runtime.setRange
    runtime.setRange = setRange
    try {
      renderPaper(detail(), "en")
      expect(document.querySelectorAll(".paper-stat")).toHaveLength(4)
      expect(document.querySelector(".paper-node[data-selected='true'] .paper-node-name")?.textContent).toBe("Tokyo")
      expect(document.querySelectorAll(".chart-block")).toHaveLength(4)
      expect(document.body.textContent).not.toContain("1.2.3")
      const oneHour = document.querySelector<HTMLButtonElement>(".paper-range-chip[data-seconds='3600']")!
      oneHour.click()
      expect(setRange).toHaveBeenCalledWith({ mode: "relative", seconds: 3600 })
      expect(oneHour.disabled).toBe(true)
      document.querySelector<HTMLButtonElement>(".paper-tab:last-child")!.click()
      expect(document.querySelectorAll(".paper-network-card")).toHaveLength(2)
      expect(document.querySelectorAll(".network-canvas")).toHaveLength(2)
    } finally { runtime.setRange = previous }
  })

  it("filters the node list by group and search without losing the selected node", () => {
    renderPaper(detail(), "en")
    const group = Array.from(document.querySelectorAll<HTMLButtonElement>(".paper-group")).find((button) => button.textContent?.startsWith("Europe"))
    group?.click()
    expect(document.querySelectorAll(".paper-node")).toHaveLength(1)
    expect(document.querySelector(".paper-node-name")?.textContent).toBe("Berlin")
    const search = document.querySelector<HTMLInputElement>(".paper-search-input")!
    search.value = "missing"
    search.dispatchEvent(new Event("input", { bubbles: true }))
    expect(document.querySelector(".paper-node-empty")?.textContent).toBe("No nodes match this filter.")
    expect(document.querySelector(".paper-detail-title")?.textContent).toBe("Tokyo")
  })

  it("switches appearance locally and falls back when a range request is rejected", () => {
    const previous = runtime.setRange
    runtime.setRange = vi.fn(() => false)
    try {
      renderPaper(detail(), "en")
      document.querySelector<HTMLButtonElement>(".paper-theme-toggle")!.click()
      expect(document.documentElement.dataset.theme).toBe("dark")
      const oneHour = document.querySelector<HTMLButtonElement>(".paper-range-chip[data-seconds='3600']")!
      oneHour.click()
      expect(oneHour.disabled).toBe(false)
      expect(state.pendingRange).toBeNull()
    } finally { runtime.setRange = previous }
  })
})

function node(id: string, name: string, country: string, group: string) {
  return {
    id, name, alias: name, country_code: country, group_id: group, status: "online", tags: [], public_remark: "", last_seen: "2026-11-08T12:00:00Z",
    system: { os: "Debian GNU/Linux", os_version: "13", arch: "x86_64", kernel: "6.12", cpu_model: "AMD EPYC", agent_version: "1.2.3" },
    metrics: { cpu: 20, cpu_total: 4, memory: 40, memory_used: 512, memory_total: 1024, disk: 30, disk_used: 300, disk_total: 1000,
      net_recv: 2048, net_sent: 1024, net_recv_total: 4096, net_sent_total: 2048, load1: .2, load5: .1, load15: .1, uptime: 3660 },
    traffic: { used: 500, limit: 1000, percentage: 50 },
  }
}

function dashboard() {
  const nodes = [node("node-1", "Tokyo", "JP", "asia"), node("node-2", "Berlin", "DE", "europe")]
  return { timestamp: "2026-11-08T12:00:00Z", route: { kind: "dashboard" }, site: { site_title: "Beat Monitor" }, locale: "en", theme: "light",
    groups: [{ id: "asia", name: "Asia", sort_order: 0 }, { id: "europe", name: "Europe", sort_order: 1 }], nodes, network: { visible: true, tasks: [] }, state: {} }
}

function detail() {
  const base = dashboard()
  const now = 1_760_000_000
  return { ...base, route: { kind: "node", node_id: "node-1" }, range: { mode: "relative", seconds: 21600 },
    range_options: [300, 900, 3600, 21600, 86400, 604800, 2592000],
    history: { from: now - 21600, to: now, series: {
      cpu: [{ timestamp: now - 100, value: 10 }, { timestamp: now, value: 20 }],
      memory: [{ timestamp: now - 100, value: 30 }, { timestamp: now, value: 40 }],
      disk: [{ timestamp: now - 100, value: 20 }, { timestamp: now, value: 30 }],
      net_recv: [{ timestamp: now - 100, value: 1000 }, { timestamp: now, value: 2048 }],
      net_sent: [{ timestamp: now - 100, value: 500 }, { timestamp: now, value: 1024 }],
    }, series_info: { cpu: { unit: "percent" }, memory: { unit: "percent" }, disk: { unit: "percent" } } },
    network: { visible: true, tasks: [
      { id: "icmp", name: "ICMP", type: "icmp", nodes: [{ node_id: "node-1", latest: { timestamp: "2026-11-08T12:00:00Z", success: true, latency_ms: 18 } }] },
      { id: "tcp", name: "TCP", type: "tcp", nodes: [{ node_id: "node-1", latest: null }] },
    ] },
    network_history: { from: now - 21600, to: now, tasks: [
      { id: "icmp", points: [{ timestamp: now - 100, latency_ms: 18 }, { timestamp: now, latency_ms: 20 }] },
      { id: "tcp", points: [] },
    ] },
  }
}
