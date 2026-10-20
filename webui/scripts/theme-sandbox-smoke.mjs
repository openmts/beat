import assert from "node:assert/strict"
import { createHash } from "node:crypto"
import { chmod, mkdir } from "node:fs/promises"
import { parseArgs } from "node:util"

const { values } = parseArgs({ options: {
  server: { type: "string" }, output: { type: "string" },
  playwright: { type: "string" }, chromium: { type: "string" },
} })
if (!values.server || !values.output || !values.playwright) throw new Error("Pass --server, --output and --playwright (--chromium for a specific browser)")
const address = new URL(values.server)
if (!["127.0.0.1", "localhost", "[::1]"].includes(address.hostname)) throw new Error("Only the local temporary fixture is supported")
const base = address.origin
const infoResponse = await fetch(`${base}/fixture/info`)
const info = await infoResponse.json()
if (info.fixture !== "beat-theme-browser-fixture") throw new Error("The target is not the owned theme test fixture")
await mkdir(values.output, { recursive: true, mode: 0o700 })
const { chromium } = await import(values.playwright)
const browser = await chromium.launch({ executablePath: values.chromium || undefined, headless: true, args: ["--no-sandbox"] })

async function verifyPublic(context) {
  const page = await context.newPage()
  const failures = []
  page.on("pageerror", (error) => failures.push(error.message))
  await page.goto(base, { waitUntil: "networkidle" })
  const frame = page.frameLocator("iframe")
  await frame.locator("body[data-snapshots]").waitFor()
  const snapshot = JSON.parse(await frame.locator("#snapshot").innerText())
  assert.equal(snapshot.site.site_title, "Beat theme fixture")
  assert.equal(snapshot.nodes.length, 1)
  for (const forbidden of ["IP_MUST_BE_HIDDEN", "PRIVATE_KEY_SENTINEL", "PRIVATE_REMARK_SENTINEL", "TOKEN_SENTINEL", "HIDDEN_NODE"]) assert.ok(!JSON.stringify(snapshot).includes(forbidden))
  for (const field of ["cookies", "storage", "network"]) assert.equal(await frame.locator("body").getAttribute(`data-${field}`), "blocked")
  assert.equal(await frame.locator("body").getAttribute("data-module"), "loaded")
  assert.equal(await frame.locator("body").evaluate(async () => { await document.fonts.ready; return document.fonts.check("16px Fixture") }), true)
  const previous = Number(await frame.locator("body").getAttribute("data-snapshots"))
  await page.waitForTimeout(5200)
  assert.ok(Number(await frame.locator("body").getAttribute("data-snapshots")) > previous)
  await privateScreenshot(page, { path: `${values.output}/dashboard.png`, fullPage: true })
  await frame.locator("#navigate").click()
  await page.waitForURL(`${base}/node/public-node`)
  await page.frameLocator("iframe").locator("body[data-snapshots]").waitFor()
  await page.frameLocator("iframe").locator("#snapshot").filter({ hasText: '"history"' }).waitFor()
  const detail = JSON.parse(await page.frameLocator("iframe").locator("#snapshot").innerText())
  assert.equal(detail.route.node_id, "public-node")
  assert.ok(detail.history.series.cpu)
  assert.ok(detail.history.series.cpu.some((point) => point.value === null))
  assert.equal(detail.history.series_info.cpu.aggregation, "avg")
  assert.equal(detail.history.series_info.cpu.unit, "percent")
  assert.ok(detail.history.series.cpu.every((point) => point.timestamp >= detail.history.from && point.timestamp <= detail.history.to))
  assert.equal(detail.history.series.private_metric, undefined)
  const previousCPU = detail.nodes[0].metrics.cpu
  await page.waitForTimeout(5200)
  const refreshedDetail = JSON.parse(await page.frameLocator("iframe").locator("#snapshot").innerText())
  assert.ok(refreshedDetail.nodes[0].metrics.cpu > previousCPU, "Node summary must continue refreshing behind the sandbox")
  await privateScreenshot(page, { path: `${values.output}/detail.png`, fullPage: true })
  await page.frameLocator("iframe").locator("#fail").click()
  await page.locator('[data-theme-fallback="runtime_error"]').waitFor()
  assert.equal(await page.locator("iframe").count(), 0)
  await privateScreenshot(page, { path: `${values.output}/fallback.png`, fullPage: true })
  assert.deepEqual(failures, [])
}

async function verifyDirect(context) {
  const page = await context.newPage()
  const response = await page.goto(`${base}${info.entry}`, { waitUntil: "networkidle" })
  assert.ok(response.headers()["content-security-policy"].includes("sandbox allow-scripts"))
  assert.equal(await page.locator("body").getAttribute("data-cookies"), "blocked")
  assert.equal(await page.locator("body").getAttribute("data-storage"), "blocked")
  assert.equal(await page.locator("body").getAttribute("data-module"), "loaded")
  await privateScreenshot(page, { path: `${values.output}/direct.png`, fullPage: true })
}

async function verifyUpload(context) {
  const page = await context.newPage()
  await page.goto(`${base}/admin/settings`, { waitUntil: "networkidle" })
  assert.equal(await page.locator("iframe").count(), 0)
  const archive = await (await context.request.get(`${base}/fixture/upload-archive`)).body()
  await page.getByRole("button", { name: "Upload theme", exact: true }).click()
  await page.locator('input[type="file"]').setInputFiles({ name: "browser-upload.theme.zip", mimeType: "application/zip", buffer: archive })
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.url().endsWith("/api/v1/admin/themes/upload") && result.request().method() === "POST"),
    page.getByRole("dialog").locator('button[type="submit"]').click(),
  ])
  assert.equal(response.status(), 201)
  assert.ok(response.request().headers()["content-type"].startsWith("application/zip"))
  const result = await response.json()
  assert.equal(result.data.archive_digest, createHash("sha256").update(archive).digest("hex"))
  assert.equal(result.data.archive_bytes, archive.length)
  assert.ok(!result.data.is_active)
  await page.getByText("browser-upload", { exact: true }).waitFor()
  await privateScreenshot(page, { path: `${values.output}/upload.png`, fullPage: true })
}

try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  await context.addCookies([{ name: "fixture_admin_cookie", value: "SYNTHETIC_ONLY", url: base }])
  await verifyPublic(context)
  await verifyDirect(context)
  await verifyUpload(context)
  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true })
  const page = await mobile.newPage()
  await page.goto(base, { waitUntil: "networkidle" })
  await page.frameLocator("iframe").locator("body[data-snapshots]").waitFor()
  await privateScreenshot(page, { path: `${values.output}/mobile.png`, fullPage: true })
  console.log(JSON.stringify({ passed: ["opaque bridge", "ES modules", "WOFF2 CORS", "privacy", "heartbeat", "public navigation", "history", "fallback", "direct URL isolation", "real UI raw ZIP upload", "mobile"], screenshots: values.output }))
} finally {
  await browser.close()
  await fetch(`${base}/fixture/stop`, { method: "POST" })
}

async function privateScreenshot(target, options) {
  await target.screenshot(options)
  await chmod(options.path, 0o600)
}
