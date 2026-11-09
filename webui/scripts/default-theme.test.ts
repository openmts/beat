import { beforeEach, describe, expect, it, vi } from "vitest"
import { countryNode, detailHeading, statusPill, trafficPercentage, usedTotal } from "../../themes/default/dist/components.js"
import { dashboard } from "../../themes/default/dist/dashboard.js"
import { renderDetail } from "../../themes/default/dist/detail.js"
import { append, element } from "../../themes/default/dist/dom.js"
import { clampPercent, decimal, formatBytes, formatDate, formatDuration, formatRate, percentValue, shortDate } from "../../themes/default/dist/format.js"
import { strings } from "../../themes/default/dist/i18n.js"
import { state } from "../../themes/default/dist/state.js"
import { runtime } from "../../themes/default/dist/runtime.js"

describe("default theme formatting", () => {
  beforeEach(() => {
    state.locale = "en"
  })

  it("formats numbers, rates and percentages consistently", () => {
    expect(decimal(12.345, 2)).toBe("12.35")
    expect(decimal(Number.NaN)).toBe("—")
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(1536)).toBe("1.50 KB")
    expect(formatBytes(-1)).toBe("—")
    expect(formatRate(1536)).toBe("1.50 KB/s")
    expect(formatRate(-1)).toBe("—")
    expect(percentValue(42.25)).toBe("42.3%")
    expect(percentValue(Number.NaN)).toBe("—")
    expect(clampPercent(120)).toBe(100)
    expect(clampPercent(-1)).toBe(0)
    expect(clampPercent(Number.NaN)).toBe(0)
  })

  it("localises durations and dates with safe fallbacks", () => {
    expect(formatDuration(90061)).toBe("1d 1h")
    expect(formatDuration(3660)).toBe("1h 1m")
    expect(formatDuration(45)).toBe("45s")
    expect(formatDuration(-1)).toBe("")
    state.locale = "zh-CN"
    expect(formatDuration(3660)).toBe("1小时 1分")
    expect(formatDate("not-a-date", {})).toBe("—")
    expect(shortDate("2026-11-07T12:34:00Z")).toContain("11/07")
    expect(strings("unknown").loading).toBe("Loading fleet data…")
    expect(strings("ja").cpu).toBe("CPU")
  })
})

describe("default theme components", () => {
  beforeEach(() => {
    state.locale = "en"
    document.body.replaceChildren()
  })

  it("renders only valid public country badges", () => {
    expect(countryNode({ country_code: "jp" })).toBeNull()
    const badge = countryNode({ country_code: "JP" })
    expect(badge?.textContent).toBe("JP")
    expect(badge?.getAttribute("aria-label")).toBe("Country JP")
  })

  it("renders online and offline status labels", () => {
    expect(statusPill({ status: "online", metrics: { uptime: 3660 } }, "en")?.textContent).toContain("Online 1h 1m")
    expect(statusPill({ status: "offline", last_seen: "2026-11-07T12:34:00Z" }, "en")?.textContent).toContain("Offline")
  })

  it("handles traffic limits and missing values", () => {
    expect(trafficPercentage({ used: 50, limit: 200 })).toBe(25)
    expect(trafficPercentage({ percentage: 33, used: 50, limit: 200 })).toBe(33)
    expect(trafficPercentage({})).toBeNaN()
    expect(usedTotal(undefined, undefined, "en")).toBe("—")
    expect(usedTotal(512, 1024, "en")).toBe("512 B / 1.00 KB")
  })

  it("does not expose agent metadata in the detail heading", () => {
    const heading = detailHeading({ name: "Tokyo", country_code: "JP", status: "online", system: { agent_version: "1.2.3" } }, "en")
    expect(heading.textContent).toContain("Tokyo")
    expect(heading.textContent).not.toContain("1.2.3")
  })
})

describe("default theme dashboard", () => {
  beforeEach(() => {
    state.locale = "en"
    document.body.innerHTML = '<div id="app"></div>'
  })

  it("renders cards and updates the document title", () => {
    dashboard(snapshot(), "en")
    expect(document.querySelectorAll(".node-card")).toHaveLength(1)
    expect(document.querySelector(".country-flag")?.textContent).toBe("US")
    expect(document.querySelector(".node-name")?.textContent).toBe("Seattle")
    expect(document.title).toBe("Beat Monitor · Beat Monitor")
  })

  it("renders empty and error states", () => {
    dashboard({ ...snapshot(), nodes: [], state: { nodes: "error" } }, "en")
    expect(document.querySelector(".status-message")?.textContent).toBe("Public data is temporarily unavailable.")
    expect(document.querySelector(".empty-state")).not.toBeNull()
  })
})

describe("default theme detail ranges", () => {
  beforeEach(() => {
    state.locale = "en"
    state.tab = "resources"
    state.pendingRange = null
    document.body.innerHTML = '<div id="app"></div>'
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null)
  })

  it("renders interactive ranges and marks a pending selection", () => {
    const setRange = vi.fn(() => true)
    const previous = runtime.setRange
    runtime.setRange = setRange
    try {
      const detail = detailSnapshot()
      renderDetail(detail, "en")
      expect(document.documentElement.dataset.route).toBe("detail")
      expect(document.querySelector(".range-chip[data-active='true']")?.textContent).toBe("6 hours")
      const sixHours = document.querySelector<HTMLButtonElement>(".range-chip[data-seconds='21600']")!
      sixHours.click()
      expect(setRange).toHaveBeenCalledWith({ mode: "relative", seconds: 21600 })
      expect(sixHours.disabled).toBe(true)
      expect(sixHours.dataset.pending).toBe("true")
    } finally { runtime.setRange = previous }
  })

  it("restores controls when the host rejects a range", () => {
    const previous = runtime.setRange
    runtime.setRange = vi.fn(() => false)
    try {
      renderDetail(detailSnapshot(), "en")
      const oneHour = document.querySelector<HTMLButtonElement>(".range-chip[data-seconds='3600']")!
      oneHour.click()
      expect(oneHour.disabled).toBe(false)
      expect(oneHour.dataset.pending).toBeUndefined()
      expect(state.pendingRange).toBeNull()
    } finally { runtime.setRange = previous }
  })

  it("renders network latency history as a chart beside the latest status", () => {
    state.tab = "network"
    const now = 1_760_000_000
    const snapshot = Object.assign(detailSnapshot(), {
      network: { visible: true, tasks: [{
        id: "icmp", name: "ICMP latency", type: "icmp",
        nodes: [{ node_id: "node-1", latest: { timestamp: new Date(now * 1000).toISOString(), success: true, latency_ms: 18.4 } }],
      }] },
      network_history: { from: now - 21600, to: now, tasks: [{ id: "icmp", points: [
        { timestamp: now - 21600, latency_ms: 20 }, { timestamp: now - 3600, latency_ms: null }, { timestamp: now, latency_ms: 18.4 },
      ] }] },
    })
    renderDetail(snapshot as never, "en")
    expect(document.querySelectorAll(".network-card")).toHaveLength(1)
    expect(document.querySelectorAll(".network-canvas")).toHaveLength(1)
    expect(document.querySelector(".network-chart-note")?.textContent).toBe("Latest 18.4 ms · Peak 20.0 ms")
    expect(document.querySelector(".network-time")?.textContent).toContain("Reachable · 18.4 ms")
    expect(document.querySelector(".range-chip[data-active='true']")?.textContent).toBe("6 hours")
  })

  it("renders three carrier series with independent legend colours", () => {
    state.tab = "network"
    const now = 1_760_000_000
    const snapshot = Object.assign(detailSnapshot(), {
      network: { visible: true, tasks: [{
        id: "carriers", name: "Three carriers", type: "icmp",
        targets: [{ id: "mobile" }, { id: "unicom" }, { id: "telecom" }],
        nodes: [{ node_id: "node-1", latest: { timestamp: new Date(now * 1000).toISOString(), success: true, latency_ms: 18.4 } }],
      }] },
      network_history: { from: now - 21600, to: now, tasks: [{ id: "carriers", series: [
        { id: "mobile", label: "移动", carrier: "mobile", points: [{ timestamp: now - 3600, latency_ms: 18 }, { timestamp: now, latency_ms: 20 }] },
        { id: "unicom", label: "联通", carrier: "unicom", points: [{ timestamp: now - 3600, latency_ms: 28 }, { timestamp: now, latency_ms: 30 }] },
        { id: "telecom", label: "电信", carrier: "telecom", points: [{ timestamp: now - 3600, latency_ms: 38 }, { timestamp: now, latency_ms: 40 }] },
      ] }] },
    })
    renderDetail(snapshot as never, "en")
    const chart = document.querySelector<HTMLElement>(".network-chart")
    expect(chart?.dataset.series).toBe("3")
    expect(chart?.dataset.carriers).toBe("mobile,unicom,telecom")
    expect(Array.from(document.querySelectorAll(".network-chart .legend-item")).map((item) => item.textContent)).toEqual(["移动", "联通", "电信"])
  })

  it("shows a no-sample network chart when the selected range has no points", () => {
    state.tab = "network"
    const snapshot = Object.assign(detailSnapshot(), {
      network: { visible: true, tasks: [{ id: "icmp", name: "ICMP latency", type: "icmp", nodes: [{ node_id: "node-1", latest: null }] }] },
      network_history: { from: 1_759_978_400, to: 1_760_000_000, tasks: [{ id: "icmp", points: [] }] },
    })
    renderDetail(snapshot as never, "en")
    expect(document.querySelector(".network-chart-note")?.textContent).toBe("No samples")
  })
})

describe("default theme DOM helpers", () => {
  it("creates text nodes without interpreting markup", () => {
    const root = element("div", "root")
    append(root, element("span", "", "<b>safe</b>"))
    expect(root.innerHTML).toBe('<span>&lt;b&gt;safe&lt;/b&gt;</span>')
  })
})

function snapshot() {
  return {
    site: { site_title: "Beat Monitor" }, groups: [{ id: "g", name: "Production" }], nodes: [{
      id: "node-1", name: "Seattle", country_code: "US", group_id: "g", status: "online",
      system: { arch: "x86_64", platform: "linux", virtualization: "kvm" },
      metrics: { cpu: 20, memory: 40, disk: 60, net_recv: 1024, net_sent: 512, net_recv_total: 2048, net_sent_total: 1024, uptime: 60 },
      traffic: { used: 512, limit: 1024 },
    }], state: { nodes: "ready" },
  }
}

function detailSnapshot() {
  const base = snapshot()
  const now = 1_760_000_000
  return {
    ...base,
    route: { kind: "node", node_id: "node-1" },
    range: { mode: "relative", seconds: 21600 },
    range_options: [300, 900, 3600, 21600, 86400, 604800, 2592000],
    history: { from: now - 21600, to: now, series: { cpu: [{ timestamp: now - 21600, value: 10 }, { timestamp: now, value: 20 }] },
      series_info: { cpu: { aggregation: "avg", unit: "percent" } } },
  }
}
