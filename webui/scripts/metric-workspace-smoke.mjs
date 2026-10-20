import assert from "node:assert/strict"
import { chmod, mkdir, writeFile } from "node:fs/promises"
import path from "node:path"

// 指标工作区的宿主浏览器冒烟：用真实账号建一个一次性看板，渲染 Grafana 对齐后的
// 结果帧、宽表、覆盖项与逐序列阈值，然后断言渲染、控制台与网络都没有回归。
const [base, output] = process.argv.slice(2)
if (!base || !output) throw new Error("usage: node metric-workspace-smoke.mjs BASE_URL OUTPUT_DIR")
const user = process.env.BEAT_SMOKE_USER
const password = process.env.BEAT_SMOKE_PASSWORD
if (!user || !password) throw new Error("BEAT_SMOKE_USER and BEAT_SMOKE_PASSWORD are required")
const origin = process.env.BEAT_SMOKE_ORIGIN || base

const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })
const context = await browser.newContext({ viewport: { width: 1440, height: 1200 } })
await context.addInitScript(() => { localStorage.setItem("locale", "en") })

const failures = [], consoleErrors = [], failedResponses = []
let checks = 0
function check(name, ok, detail = "") {
  checks += 1
  if (!ok) failures.push(`${name}${detail ? ` :: ${detail}` : ""}`)
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${ok || !detail ? "" : ` :: ${detail}`}`)
}

// 面板只在查询结果到达后才写「上次成功查询」并出图；断言前先等这一步，
// 避免把「还在画」判成回归。加有界轮询而不是固定 sleep，失败时仍然会如实报错。
async function waitForPanelQuery(title, timeoutMs = 30000) {
  const region = page.getByRole("region", { name: title })
  const marker = region.getByText(/Last successful query/)
  await marker.first().waitFor({ state: "attached", timeout: timeoutMs }).catch((error) => {
    console.log(`WARN ${title} query marker missing: ${String(error).split(/\n/)[0]}`)
  })
}

async function waitForMinimum(locator, minimum, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  let count = await locator.count()
  while (count < minimum && Date.now() < deadline) {
    await page.waitForTimeout(200)
    count = await locator.count()
  }
  return count
}
const api = (method, route, data) => context.request.fetch(`${base}${route}`, { method, headers: { Origin: origin }, data })
const apiData = async (method, route, data) => (await (await api(method, route, data)).json()).data

let dashboardID = null
let dashboardRevision = 1
const page = await context.newPage()
page.on("pageerror", (error) => consoleErrors.push(error.message))
page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(message.text()) })
page.on("response", (response) => {
  if (response.status() >= 400 && !response.url().endsWith("/favicon.ico")) failedResponses.push(`${response.status()} ${response.url()}`)
})

try {
  const login = await api("POST", "/api/v1/auth/login", { username: user, password })
  check("session login", login.status() === 200, `status=${login.status()}`)
  const nodeID = await smokeNodeID()
  check("node with series found", Boolean(nodeID), `node=${nodeID}`)
  const created = await api("POST", "/api/v1/admin/metrics/dashboards", { definition: smokeDefinition(nodeID) })
  const createdBody = await created.text()
  const createdStatus = created.status()
  const createdRecord = createdStatus === 201 ? JSON.parse(createdBody).data : null
  check("smoke dashboard created", createdRecord !== null, `status=${createdStatus} body=${createdBody.slice(0, 200)}`)
  if (!createdRecord) throw new Error(`smoke dashboard creation failed: ${createdStatus} ${createdBody.slice(0, 200)}`)
  dashboardID = createdRecord.id
  dashboardRevision = createdRecord.revision ?? 1

  await page.goto(`${base}/admin/metrics?dashboard=${dashboardID}`)
  await page.getByRole("region", { name: "Smoke result frame" }).waitFor({ timeout: 30000 })
  for (const title of ["Smoke result frame", "Smoke wide table", "Smoke overrides", "Smoke thresholds"]) {
    await waitForPanelQuery(title)
  }
  await renderChecks()
  check("no browser runtime errors", consoleErrors.length === 0, consoleErrors.slice(0, 3).join(" | "))
  check("no failed API responses", failedResponses.length === 0, failedResponses.slice(0, 3).join(" | "))
  await writeFile(path.join(output, "results.json"), JSON.stringify({ dashboardID, checks, failures, consoleErrors, failedResponses }, null, 2), { mode: 0o600 })
  assert.deepEqual(failures, [], "metric workspace smoke failures")
  console.log(`Metric workspace browser checks passed (${checks} checks); artifacts: ${output}`)
} catch (error) {
  await screenshot("failure.png").catch(() => {})
  await writeFile(path.join(output, "results.json"), JSON.stringify({ dashboardID, checks, failures, consoleErrors, failedResponses, error: String(error) }, null, 2), { mode: 0o600 }).catch(() => {})
  throw error
} finally {
  if (dashboardID) await api("DELETE", `/api/v1/admin/metrics/dashboards/${dashboardID}?revision=${dashboardRevision}`).catch(() => {})
  await browser.close()
}

// 面板里的 node_id 可能是 `$node` 这类变量引用；直接拿去建看板会被校验回
// `series variable is not defined`，因此只接受真实存在且已纳管的节点 id。
async function smokeNodeID() {
  const nodes = await apiData("GET", "/api/v1/nodes/manage")
  const managed = new Set((nodes.nodes ?? nodes ?? []).map((node) => node.id))
  const records = await apiData("GET", "/api/v1/admin/metrics/dashboards")
  for (const record of records) {
    for (const panel of record.definition?.panels ?? []) {
      for (const series of panel.series ?? []) {
        if (typeof series.node_id === "string" && managed.has(series.node_id)) return series.node_id
      }
    }
  }
  return [...managed][0] ?? null
}

function smokeDefinition(nodeID) {
  const series = (refID, metric, label, color) => ({ ref_id: refID, node_id: nodeID, metric, aggregation: "avg", label, color })
  const basePanel = { size: "large", legend: false, thresholds: [] }
  return { version: 1, title: "Smoke Grafana parity", description: "host browser smoke", time_range_seconds: 21600, refresh_seconds: 0, variables: [], panels: [
    { ...basePanel, id: "frame", title: "Smoke result frame", type: "table", series: [series("A", "cpu", "Smoke CPU", "blue"), series("B", "memory", "Smoke MEM", "green")],
      transformations: [{ kind: "reduce", mode: "series_to_rows", calculations: ["min", "max", "mean"] }] },
    { ...basePanel, id: "wide", title: "Smoke wide table", type: "table", table_orientation: "wide", series: [series("A", "cpu", "Smoke CPU", "blue"), series("B", "memory", "Smoke MEM", "green")] },
    { ...basePanel, id: "overrides", title: "Smoke overrides", type: "timeseries", legend: true, series: [series("A", "cpu", "Smoke CPU", "blue"), series("B", "memory", "Smoke MEM", "green")],
      overrides: [{ matcher: { type: "by_ref_id", value: "B" }, color: "violet", line_width: 3, axis_placement: "right", axis_label: "Mem axis" }] },
    { ...basePanel, id: "stat", title: "Smoke thresholds", type: "stat", size: "medium", series: [series("A", "cpu", "Smoke CPU", "blue")],
      overrides: [{ matcher: { type: "by_name", value: "Smoke CPU" }, thresholds: [{ value: 40, color: "green" }, { value: 80, color: "rose" }] }] },
  ] }
}

async function renderChecks() {
  for (const title of ["Smoke result frame", "Smoke wide table", "Smoke overrides", "Smoke thresholds"]) {
    check(`panel ${title}`, await page.getByRole("region", { name: title }).isVisible())
  }
  const frame = page.getByRole("region", { name: "Smoke result frame" })
  const frameHeaders = await frame.getByRole("columnheader").allTextContents()
  // Grafana 的 Series to rows：一列字段 + 每个计算方式一列，行数等于字段数。
  check("result frame keeps Grafana column set", ["Field", "Minimum", "Maximum", "Average"].every((label) => frameHeaders.includes(label)) && frameHeaders.length === 4, frameHeaders.join(", "))
  const frameRows = await frame.locator("tbody tr").count()
  check("result frame renders one row per field", frameRows === 2, `rows=${frameRows}`)
  const frameCells = (await frame.locator("tbody tr td").allTextContents()).map((text) => text.trim())
  check("result frame keeps one value per field and calculation", frameCells.filter((text) => text.endsWith("%")).length === 6, frameCells.join(" | "))

  const wide = page.getByRole("region", { name: "Smoke wide table" })
  const wideHeaders = await wide.getByRole("columnheader").allTextContents()
  // 列名是「序列标签 · 节点别名」，别名属于站点数据，因此只断言标签前缀与列数。
  check("wide table keeps one column per series", wideHeaders.length === 3 && wideHeaders.some((header) => header.startsWith("Smoke CPU")) && wideHeaders.some((header) => header.startsWith("Smoke MEM")), wideHeaders.join(", "))

  const overrides = page.getByRole("region", { name: "Smoke overrides" })
  const overridePaths = await waitForMinimum(overrides.locator("svg path"), 2)
  check("overrides panel draws one line per series", overridePaths >= 2, `paths=${overridePaths}`)
  const axisLabels = await waitForMinimum(overrides.getByText("Mem axis"), 1)
  check("overrides axis label renders", axisLabels > 0, `labels=${axisLabels}`)
  const stat = page.getByRole("region", { name: "Smoke thresholds" })
  check("threshold-overridden stat renders a value", (await stat.locator("section, div").last().innerText()).trim().length > 0)
  check("no workspace load failure banner", (await page.getByText("workspace.load_failed").count()) === 0)
  await screenshot("workspace.png")
  await frame.screenshot({ path: path.join(output, "result-frame.png") }).then(() => chmod(path.join(output, "result-frame.png"), 0o600))
  await wide.screenshot({ path: path.join(output, "wide-table.png") }).then(() => chmod(path.join(output, "wide-table.png"), 0o600))
}

async function screenshot(name) {
  const destination = path.join(output, name)
  await page.screenshot({ path: destination, fullPage: true })
  await chmod(destination, 0o600)
}
