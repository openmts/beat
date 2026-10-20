import assert from "node:assert/strict"
import { chmod, mkdir, writeFile } from "node:fs/promises"
import path from "node:path"

const [base, output] = process.argv.slice(2)
if (!base || !output) throw new Error("usage: node theme-declarative-smoke.mjs FIXTURE_URL OUTPUT_DIR")
const info = await (await fetch(`${base}/fixture/info`)).json()
assert.equal(info.fixture, "beat-declarative-browser-fixture", "Only the isolated fixture may be changed")
const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })
const failures = []
let page
try {
  const anonymous = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  await anonymous.addInitScript(() => { if (!localStorage.getItem("theme")) localStorage.setItem("theme", "light"); localStorage.setItem("locale", "en") })
  page = await anonymous.newPage()
  page.on("pageerror", (error) => failures.push(error.message))
  const queryRequests = [], summaryRequests = []
  page.on("request", (request) => {
    if (request.url().endsWith("/metrics/query")) queryRequests.push(request)
    if (request.url().endsWith("/nodes/node-0")) summaryRequests.push(request)
  })
  // control 是公共车队工具条上的访客视图开关：它缺省为 Cards，并覆盖声明 table 的主题
  // （公共面设计刻意保留「用户批准的卡片缺省视图」，见 public-fleet-experience-design.md），
  // 因此脚本按主题显式驱动开关，再断言主题声明的布局确实渲染出来。
  const layouts = {
    slate: { overview: "cards", detail: "balanced", control: "Cards" },
    operations: { overview: "table", detail: "compact", control: "Table" },
    canvas: { overview: "tiles", detail: "spotlight", control: "Cards" },
  }
  for (const [name, { overview, detail, control }] of Object.entries(layouts)) {
    assert.equal((await fetch(`${base}/fixture/activate/${name}`, { method: "POST" })).status, 204)
    await page.evaluate(() => localStorage.setItem("theme", "light")).catch(() => {})
    await page.goto(base)
    await page.getByRole("button", { name: control, exact: true }).click()
    await expectOverviewLayout(page, overview)
    await page.getByRole("link", { name: "Frankfurt edge" }).first().waitFor()
    assert.doesNotMatch(await page.locator("body").innerText(), /PRIVATE_|203\.0\.113\.18|NaN|Infinity/)
    await privateScreenshot(page, { path: path.join(output, `${name}.png`) })
    await page.getByRole("link", { name: "Frankfurt edge" }).first().click()
    await expectDetailLayout(page, detail)
    await page.locator(".recharts-responsive-container").first().waitFor()
    assert.equal(await page.getByRole("button", { name: /Edit panel/ }).count(), 0)
    assert.equal(await page.locator('[draggable="true"]').count(), 0)
    await privateScreenshot(page, { path: path.join(output, `${name}-detail.png`), fullPage: true })
    if (name === "slate") await verifyPublicControls(page, queryRequests, summaryRequests)
    await page.evaluate(() => localStorage.setItem("theme", "dark"))
    await page.goto(base)
    await page.getByRole("button", { name: control, exact: true }).click()
    await expectOverviewLayout(page, overview)
    assert.equal(await page.locator("[data-public-theme]").evaluate((element) => element.classList.contains("dark")), true)
    await privateScreenshot(page, { path: path.join(output, `${name}-dark.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), `${name} overflows mobile`)
    await privateScreenshot(page, { path: path.join(output, `${name}-mobile.png`), fullPage: true })
    await page.setViewportSize({ width: 1440, height: 900 })
  }
  await anonymous.close()
  const admin = await browser.newContext({ viewport: { width: 1440, height: 1000 }, extraHTTPHeaders: { "X-Beat-Fixture-Owner": "theme-preview-fixture" } })
  await admin.addInitScript(() => { localStorage.setItem("locale", "en"); localStorage.setItem("theme", "light") })
  page = await admin.newPage()
  page.on("pageerror", (error) => failures.push(error.message))
  await page.goto(`${base}/admin/settings`)
  await page.getByRole("heading", { name: "Installed themes", exact: true }).waitFor()
  const slate = page.locator('[data-slot="card"]').filter({ has: page.getByRole("heading", { name: "Slate", exact: true }) }).last()
  await slate.getByRole("button", { name: "Preview", exact: true }).click()
  const preview = page.frameLocator('iframe[title="Slate preview"]')
  await preview.getByRole("link", { name: "Frankfurt edge" }).first().waitFor()
  assert.doesNotMatch(await preview.locator("body").innerText(), /PRIVATE_|203\.0\.113\.18/)
  await page.getByRole("button", { name: "Mobile", exact: true }).click()
  assert.ok(await preview.locator("body").evaluate(() => window.innerWidth <= 390))
  await page.getByRole("button", { name: "Dark", exact: true }).click()
  assert.equal(await preview.locator("[data-public-theme]").evaluate((element) => element.classList.contains("dark")), true)
  assert.equal(await page.locator("html").evaluate((element) => element.style.getPropertyValue("--background")), "")
  await privateScreenshot(page, { path: path.join(output, "admin-preview-mobile.png"), fullPage: true })
  await preview.getByRole("link", { name: "Frankfurt edge" }).first().click()
  await expectDetailLayout(preview, "balanced")
  await page.getByLabel("Corner radius (px)").fill("0")
  await page.getByRole("button", { name: "Save settings", exact: true }).click()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  await page.getByRole("button", { name: "Reset settings", exact: true }).waitFor()
  const configured = await (await admin.request.get(`${base}/api/v1/admin/themes/${info.packages.slate.id}/preview`)).json()
  assert.equal(configured.data.declarative.radius, 0)
  const active = await (await admin.request.get(`${base}/api/v1/themes/active`)).json()
  assert.equal(active.data.package_id, info.packages.canvas.id, "Preview/config must not activate the package")
  await page.getByRole("button", { name: "Reset settings", exact: true }).click()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  await page.getByLabel("Corner radius (px)").filter({ visible: true }).waitFor()
  await page.getByRole("dialog").last().getByRole("button", { name: "Close", exact: true }).click()
  await page.getByRole("button", { name: "Use built-in", exact: true }).click()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  await page.getByRole("button", { name: "Built-in active", exact: true }).waitFor()
  await privateScreenshot(page, { path: path.join(output, "admin-library.png"), fullPage: true })
  assert.equal((await (await admin.request.get(`${base}/api/v1/themes/active`)).json()).data.built_in, true)
  await admin.close()
  assert.deepEqual(failures, [], "Unexpected browser runtime errors")
  await writeFile(path.join(output, "results.json"), JSON.stringify({ fixture: info.fixture, themes: Object.keys(layouts), runtimeErrors: failures, desktopMobileDark: true, privateDataHidden: true, previewConfigurationRecovery: true }, null, 2), { mode: 0o600 })
  console.log(`Theme browser checks passed; artifacts: ${output}`)
} catch (error) {
  if (page && !page.isClosed()) await privateScreenshot(page, { path: path.join(output, "failure.png"), fullPage: true }).catch(() => {})
  throw error
} finally {
  await browser.close()
  await fetch(`${base}/fixture/stop`, { method: "POST" })
}

/**
 * 公共车队页按分组渲染多个主题容器，因此摘要页会出现多个同布局的节点集合。
 * 这里要求“至少一个且全部”都使用主题声明的首页布局，避免只看到其中一个分组就通过。
 */
async function expectOverviewLayout(scope, overview) {
  const collections = scope.locator(`[data-theme-overview="${overview}"]`)
  await collections.first().waitFor()
  const layouts = await collections.evaluateAll((nodes) => nodes.map((node) => node.getAttribute("data-theme-overview")))
  assert.ok(layouts.length >= 1, "expected at least one themed collection")
  assert.ok(layouts.every((layout) => layout === overview), `unexpected overview layouts: ${layouts.join(",")}`)
}

/** 节点详情页只声明一个主题容器，同样容忍“恰好一个”之外的实现细节。 */
async function expectDetailLayout(scope, detail) {
  const container = scope.locator(`[data-theme-detail-layout="${detail}"]`)
  await container.first().waitFor()
}

async function privateScreenshot(target, options) {
  await target.screenshot(options)
  await chmod(options.path, 0o600)
}

async function verifyPublicControls(target, queries, summaries) {
  await target.getByRole("button", { name: /Inspect data.*CPU usage/ }).click()
  assert.ok((await target.getByRole("dialog").getByRole("link").first().getAttribute("href")).startsWith("/node/"))
  const [download] = await Promise.all([target.waitForEvent("download"), target.getByRole("button", { name: "Download CSV", exact: true }).click()])
  const csv = path.join(output, "public-cpu.csv")
  await download.saveAs(csv)
  await chmod(csv, 0o600)
  await target.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click()
  await target.getByRole("combobox", { name: "Time range" }).click()
  const nextRange = target.waitForResponse((response) => {
    if (!response.url().endsWith("/metrics/query") || response.request().method() !== "POST") return false
    const query = response.request().postDataJSON()
    return Date.parse(query.to) - Date.parse(query.from) === 21600000
  })
  await target.getByRole("option", { name: "Last 6 hours" }).click()
  assert.equal((await nextRange).status(), 200)
  await target.getByRole("button", { name: "Pause refresh" }).click()
  await target.waitForLoadState("networkidle")
  const count = { query: queries.length, summary: summaries.length }
  await target.waitForTimeout(5500)
  assert.equal(queries.length, count.query, "Paused workspace must not poll")
  assert.equal(summaries.length, count.summary, "Paused summary must not poll")
  const refreshed = target.waitForResponse((response) => response.url().endsWith("/metrics/query"))
  await target.getByRole("button", { name: "Refresh now" }).click()
  assert.equal((await refreshed).status(), 200)
  await privateScreenshot(target, { path: path.join(output, "public-workspace-controls.png"), fullPage: true })
}
