import assert from "node:assert/strict"
import { chmod, mkdir, readFile } from "node:fs/promises"
import { createServer } from "node:http"
import { extname, join } from "node:path"
import { parseArgs } from "node:util"

const { values } = parseArgs({ options: {
  output: { type: "string" }, playwright: { type: "string" }, chromium: { type: "string" },
} })
if (!values.output) throw new Error("Pass --output")
const repo = join(import.meta.dirname, "..", "..")
const bridge = `const frame=document.getElementById('theme');const query=new URLSearchParams(location.search);const route=query.get('route')||'dashboard';const theme=query.get('theme')||'light';let instance='';let snapshot=null;window.__rangeRequests=[];function publish(){if(!snapshot)return;frame.contentWindow.postMessage({beat:{version:3,instance_id:instance,type:'snapshot',payload:snapshot}},'*')}function start(){if(!snapshot)return;instance='a'.repeat(32);frame.contentWindow.postMessage({beat:{version:3,instance_id:instance,type:'init'}},'*')}fetch('/snapshot.json').then(r=>r.json()).then(all=>{snapshot=all[route];snapshot.theme=theme;start()});frame.addEventListener('load',start);addEventListener('message',event=>{if(event.source!==frame.contentWindow)return;const message=event.data?.beat;if(message?.instance_id!==instance||!snapshot)return;if(message.type==='ready')publish();if(message.type==='setRange'){window.__rangeRequests.push(message.payload);snapshot={...snapshot,range:message.payload};publish()}})`
await mkdir(values.output, { recursive: true, mode: 0o700 })
const { chromium } = await import(values.playwright || process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
const data = fixtures()
const server = createServer((request, response) => serve(repo, data, request, response))
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
const base = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ executablePath: values.chromium || process.env.BEAT_BROWSER_EXECUTABLE, headless: true, args: ["--no-sandbox"] })
const failures = []

try {
  await verifyDashboard(browser, base, failures)
  await verifyDetail(browser, base, failures)
  await verifyMobile(browser, base, failures)
  await verifyDark(browser, base, failures)
  assert.deepEqual(failures, [])
  console.log(JSON.stringify({ passed: ["desktop layout", "country badge", "detail charts", "range controls", "network tab", "mobile", "dark mode"], screenshots: values.output }))
} finally {
  await browser.close()
  server.close()
}

async function verifyDashboard(browserInstance, origin, errors) {
  const page = await open(browserInstance, origin, "dashboard", "light", { width: 1440, height: 1000 }, errors)
  const frame = page.frameLocator("#theme")
  await frame.locator(".fleet-grid .node-card").first().waitFor()
  assert.equal(await frame.locator(".node-card").count(), data.dashboard.nodes.length)
  assert.equal(await columns(frame.locator(".fleet-grid")), 4)
  assert.equal(await frame.locator(".country-flag").first().textContent(), "US")
  assert.equal(await frame.locator(".status-pill[data-status='online']").count(), 8)
  await screenshot(page, join(values.output, "home.png"))
  await page.close()
}

async function verifyDetail(browserInstance, origin, errors) {
  const page = await open(browserInstance, origin, "detail", "light", { width: 1440, height: 1250 }, errors)
  const frame = page.frameLocator("#theme")
  await frame.locator(".chart-block").first().waitFor()
  assert.equal(await frame.locator("body").evaluate((node) => getComputedStyle(node).backgroundColor), "rgb(255, 255, 255)")
  assert.equal(await frame.locator(".chart-canvas").count(), 4)
  assert.equal(await frame.getByText("agent 1.2.3", { exact: false }).count(), 0)
  assert.deepEqual(await frame.locator('[role="tab"]').allTextContents(), ["Resources", "Network latency"])
  assert.equal(await frame.locator(".range-chip[data-active='true']").textContent(), "6 hours")
  await frame.locator(".range-chip[data-seconds='3600']").click()
  await page.waitForFunction(() => window.__rangeRequests.length === 1)
  assert.deepEqual(await page.evaluate(() => window.__rangeRequests), [{ mode: "relative", seconds: 3600 }])
  await frame.locator(".range-chip[data-seconds='3600'][data-active='true']").waitFor()
  const canvas = await frame.locator(".chart-canvas").first().evaluate((node) => ({ width: node.width, height: node.height }))
  assert.ok(canvas.width > 0 && canvas.height > 0)
  await frame.locator('[role="tab"]').filter({ hasText: "Network latency" }).click()
  assert.equal(await frame.locator(".network-card").count(), 3)
  assert.equal(await frame.locator(".network-type").first().textContent(), "icmp")
  assert.equal(await frame.locator(".network-canvas").count(), 3)
  assert.match(await frame.locator(".network-chart-note").first().textContent(), /Latest .* ms/)
  const networkCanvas = await frame.locator(".network-canvas").first().evaluate((node) => ({ width: node.width, height: node.height }))
  assert.ok(networkCanvas.width > 0 && networkCanvas.height > 0)
  await screenshot(page, join(values.output, "detail.png"))
  await page.close()
}

async function verifyMobile(browserInstance, origin, errors) {
  const page = await open(browserInstance, origin, "dashboard", "light", { width: 390, height: 844 }, errors)
  const frame = page.frameLocator("#theme")
  await frame.locator(".node-card").first().waitFor()
  assert.equal(await columns(frame.locator(".fleet-grid")), 1)
  assert.ok(await frame.locator("body").evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1))
  await screenshot(page, join(values.output, "mobile.png"))
  await page.goto(`${origin}/harness.html?route=detail&theme=light`, { waitUntil: "networkidle" })
  await frame.locator(".detail-meta").waitFor()
  assert.equal(await columns(frame.locator(".detail-meta")), 1)
  assert.ok(await frame.locator("body").evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1))
  await screenshot(page, join(values.output, "mobile-detail.png"))
  await page.close()
}

async function verifyDark(browserInstance, origin, errors) {
  const page = await open(browserInstance, origin, "dashboard", "dark", { width: 1440, height: 1000 }, errors)
  const frame = page.frameLocator("#theme")
  await frame.locator(".node-card").first().waitFor()
  assert.equal(await frame.locator("html").getAttribute("data-theme"), "dark")
  await screenshot(page, join(values.output, "dark.png"))
  await page.close()
}

async function open(browserInstance, origin, route, theme, viewport, errors) {
  const page = await browserInstance.newPage({ viewport, deviceScaleFactor: 1 })
  page.on("pageerror", (error) => errors.push(error.message))
  page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()) })
  await page.goto(`${origin}/harness.html?route=${route}&theme=${theme}`, { waitUntil: "networkidle" })
  return page
}

async function columns(locator) {
  return locator.evaluate((node) => getComputedStyle(node).gridTemplateColumns.split(" ").length)
}

async function screenshot(page, path) {
  await page.screenshot({ path })
  await chmod(path, 0o600)
}

function fixtures() {
  const now = Math.floor(Date.now() / 1000)
  const nodes = names().map((name, index) => node(name, index, now))
  const dashboard = {
    timestamp: new Date(now * 1000).toISOString(), route: { kind: "dashboard" }, groups: [{ id: "production", name: "Production", sort_order: 0 }],
    site: { site_title: "Monitor", site_description: "Fleet overview", logo_url: "", favicon_url: "", public_address_mode: "hidden", show_network_quality: true },
    locale: "en", theme: "light", configuration: {}, nodes, network: network(nodes[0].id), state: { nodes: "ready", metrics: "ready", network: "ready" },
  }
  const detail = { ...dashboard, route: { kind: "node", node_id: nodes[0].id }, nodes: [nodes[0]], history: history(now), network_history: networkHistory(now),
    range: { mode: "relative", seconds: 21600 }, range_options: [300, 900, 3600, 21600, 86400, 604800, 2592000] }
  return { dashboard, detail }
}

function names() {
  return ["US · DMIT", "JP · Zouter", "DE · Bero", "US · CCS", "SG · CC", "HK · Alice", "US · Miao", "CA · Rubbish"]
}

function node(label, index, now) {
  const [country, name] = label.split(" · ")
  return {
    id: `node-${index}`, name, alias: name, country_code: country, group_id: "production", status: "online", tags: [], public_remark: "Managed node",
    last_seen: new Date((now - 20) * 1000).toISOString(), telemetry_at: new Date((now - 2) * 1000).toISOString(), telemetry_age_seconds: 2, telemetry_state: "fresh",
    system: { cpu_model: "AMD EPYC 9654", os: "Debian GNU/Linux", platform: "linux", os_version: "13", kernel: "6.12", arch: "x86_64", virtualization: "kvm", agent_version: "1.2.3" },
    metrics: { cpu: 12 + index, cpu_used: 1, cpu_total: 4, memory: 28 + index, memory_used: 512 << 20, memory_total: 1 << 30,
      disk: 18 + index, disk_used: 3 << 30, disk_total: 20 << 30, net_recv: 420000, net_sent: 180000, net_recv_total: 2 << 30, net_sent_total: 1 << 30,
      load1: 0.2, load5: 0.1, load15: 0.1, uptime: 86400 + index * 3600, processes: 109 },
    traffic: { used: 380 << 30, limit: 2 << 40, percentage: 18.6, status: "ok" },
  }
}

function history(now) {
  const series = {}, seriesInfo = {}
  for (const [key, base, amplitude, unit] of [["cpu", 3, 2, "percent"], ["memory", 28, 3, "percent"], ["disk", 12, 0.4, "percent"], ["net_recv", 350000, 700000, "bytes/s"], ["net_sent", 180000, 300000, "bytes/s"]]) {
    series[key] = Array.from({ length: 121 }, (_, index) => ({ timestamp: now - 21600 + index * 180, value: index === 60 ? null : base + amplitude * (0.5 + 0.5 * Math.sin(index / 7)) }))
    seriesInfo[key] = { aggregation: "avg", unit }
  }
  return { from: now - 21600, to: now, series, series_info: seriesInfo }
}

function network(nodeId) {
  const latest = (success, latency_ms, status_code) => ({ node_id: nodeId, latest: { timestamp: new Date().toISOString(), success, latency_ms, status_code } })
  return { visible: true, tasks: [
    { id: "icmp", name: "ICMP latency", type: "icmp", nodes: [latest(true, 18.4)] },
    { id: "tcp", name: "TCP connect", type: "tcp", nodes: [latest(true, 31.8, 200)] },
    { id: "http", name: "HTTP probe", type: "http", nodes: [latest(false, undefined, 503)] },
  ] }
}

function networkHistory(now) {
  const points = (offset, base, gap = -1) => Array.from({ length: 121 }, (_, index) => ({
    timestamp: now - 21600 + index * 180,
    latency_ms: index === gap ? null : base + 3 * Math.sin(index / 9) + offset,
  }))
  return { from: now - 21600, to: now, tasks: [
    { id: "icmp", points: points(0, 18, 40) },
    { id: "tcp", points: points(10, 31, 70) },
    { id: "http", points: points(20, 45, 90) },
  ] }
}

function harness() {
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="data:,"><style>html,body{margin:0;height:100%;background:#f6f6f7}iframe{display:block;width:100vw;height:100vh;border:0}</style></head><body><iframe id="theme" sandbox="allow-scripts" src="/themes/default/dist/index.html"></iframe><script src="/harness.js"></script></body></html>`
}

function serve(root, all, request, response) {
  const pathname = new URL(request.url, "http://127.0.0.1").pathname
  if (pathname === "/" || pathname === "/harness.html") return send(response, 200, "text/html; charset=utf-8", harness())
  if (pathname === "/harness.js") return send(response, 200, "text/javascript; charset=utf-8", bridge)
  if (pathname === "/snapshot.json") return send(response, 200, "application/json; charset=utf-8", JSON.stringify(all))
  if (!pathname.startsWith("/themes/default/")) return send(response, 404, "text/plain; charset=utf-8", "not found")
  readFile(join(root, pathname.slice(1))).then((content) => send(response, 200, contentType(pathname), content)).catch(() => send(response, 404, "text/plain; charset=utf-8", "not found"))
}

function contentType(pathname) {
  return { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8", ".png": "image/png" }[extname(pathname)] || "application/octet-stream"
}

function send(response, status, type, body) {
  response.writeHead(status, { "Content-Type": type, "Cache-Control": "no-store", "Access-Control-Allow-Origin": "*" })
  response.end(body)
}
