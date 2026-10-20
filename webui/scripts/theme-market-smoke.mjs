import assert from "node:assert/strict"
import { chmod, mkdir, writeFile } from "node:fs/promises"
import path from "node:path"

const [base, output] = process.argv.slice(2)
if (!base || !output) throw new Error("usage: node theme-market-smoke.mjs FIXTURE_URL OUTPUT_DIR")
const info = await (await fetch(`${base}/fixture/info`)).json()
assert.equal(info.fixture, "beat-market-browser-fixture", "Only the isolated market fixture may be changed")
const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })
const ownerHeader = { "X-Beat-Fixture-Owner": "theme-preview-fixture" }
const admin = await browser.newContext({ viewport: { width: 1440, height: 1000 }, extraHTTPHeaders: ownerHeader })
await admin.addInitScript(() => { localStorage.setItem("locale", "en"); if (!localStorage.getItem("theme")) localStorage.setItem("theme", "light") })
const page = await admin.newPage()
const failures = [], requests = [], selections = []
page.on("pageerror", (error) => failures.push(error.message))
page.on("request", (request) => {
  requests.push(request.url())
  if (request.url().endsWith("/install") && request.method() === "POST") selections.push(request.postDataJSON())
})
let sourceID
try {
  await page.goto(`${base}/admin/settings`)
  await page.getByRole("heading", { name: "Installed themes", exact: true }).waitFor()
  await addSource()
  await verifyCache()
  const first = await installFirst()
  const second = await upgradeAndRestore(first)
  await captureMarketLayouts()
  await verifyPendingCleanup(second, first)
  await removeSource()
  assert.equal(requests.some((url) => new URL(url).origin === new URL(info.catalog_url).origin), false, "Browser bypassed the verified market client")
  assert.deepEqual(failures, [], "Unexpected browser runtime errors")
  await writeFile(path.join(output, "results.json"), JSON.stringify({ fixture: info.fixture, sourceID, first: first.id, restored: second.id,
    signedCacheAnd304: true, staleFallback: true, recentAuthentication: true, selections, upgradeRollbackRestore: true,
    configurationPreserved: true, explicitCleanupPending: true, desktopMobileDark: true, runtimeErrors: failures }, null, 2), { mode: 0o600 })
  console.log(`Theme market browser checks passed; artifacts: ${output}`)
} catch (error) {
  await screenshot("failure.png").catch(() => {})
  throw error
} finally {
  await browser.close()
  await fetch(`${base}/fixture/stop`, { method: "POST" })
}

function sourceCard() { return page.getByRole("article", { name: "Fixture publisher", exact: true }) }
function packageCard(version) {
  return page.locator('[data-slot="card"]').filter({ has: page.getByRole("heading", { name: "Market Slate", exact: true }) })
    .filter({ has: page.getByText(version, { exact: true }) }).last()
}
async function jsonAPI(route) { return (await (await admin.request.get(`${base}/api/v1${route}`)).json()).data }
async function control(route) { assert.equal((await admin.request.post(`${base}/fixture${route}`)).status(), 204) }
async function screenshot(name) {
  await page.waitForLoadState("networkidle")
  const destination = path.join(output, name)
  const marketCard = page.locator('[data-slot="card"]').filter({ has: page.getByRole("heading", { name: "Theme market", exact: true }) }).last()
  if (name !== "failure.png" && await marketCard.isVisible()) {
    await marketCard.evaluate((element) => element.scrollIntoView({ block: "start" }))
    await page.evaluate(() => window.scrollBy(0, -80))
    await marketCard.screenshot({ path: destination })
  } else await page.screenshot({ path: destination, fullPage: true })
  await chmod(destination, 0o600)
}
async function tab(name) { await page.getByRole("tab", { name, exact: true }).click() }
async function waitForOperation() { await page.getByRole("dialog").waitFor({ state: "hidden" }) }

async function addSource() {
  await tab("Market")
  await page.getByRole("button", { name: "Add source", exact: true }).click()
  await page.getByLabel("Source name").fill("Fixture publisher")
  await page.getByLabel("Catalog URL").fill(info.catalog_url)
  await page.getByLabel("Public key", { exact: true }).fill(info.public_key)
  await page.getByLabel("Key fingerprint").fill(info.source_id)
  const created = page.waitForResponse((response) => response.url().endsWith("/admin/theme-markets") && response.request().method() === "POST")
  await page.getByRole("dialog").getByRole("button", { name: "Add source", exact: true }).click()
  const response = await created
  assert.equal(response.status(), 201)
  sourceID = (await response.json()).data.id
  await waitForOperation()
  await sourceCard().getByText(info.source_id, { exact: true }).waitFor()
}

async function refresh() {
  const response = page.waitForResponse((value) => value.url().endsWith(`/theme-markets/${sourceID}/catalog`))
  await sourceCard().getByRole("button", { name: "Refresh catalog", exact: true }).click()
  const result = await response
  assert.equal(result.status(), 200)
  await waitForOperation()
  return (await result.json()).data
}

async function verifyCache() {
  const first = await refresh()
  assert.equal(first.cached, false)
  assert.equal(first.source_id, info.source_id)
  assert.equal(first.themes.length, 1)
  const cached = await refresh()
  assert.equal(cached.cached, true)
  assert.equal(cached.catalog_digest, first.catalog_digest)
  const latestInfo = await (await fetch(`${base}/fixture/info`)).json()
  assert.ok(latestInfo.not_modified >= 1, "Fixture did not exercise a real signed-cache 304")
  await control("/catalog/offline")
  const stale = await refresh()
  assert.equal(stale.stale, true)
  assert.equal(stale.error_category, "unavailable")
  await sourceCard().getByText("Source unavailable", { exact: true }).waitFor()
  await screenshot("market-stale-cache.png")
  await control("/catalog/v1")
  await refresh()
}

async function installFirst() {
  await control("/require-proof")
  const rejected = page.waitForResponse((response) => response.url().endsWith("/install"))
  await sourceCard().getByRole("button", { name: "Install", exact: true }).click()
  assert.equal((await rejected).status(), 428)
  await page.getByLabel("Current password").fill("fixture-owner-proof")
  const installed = page.waitForResponse((response) => response.url().endsWith("/install"))
  await page.getByRole("button", { name: "Verify and continue", exact: true }).click()
  const response = await installed
  assert.equal(response.status(), 201)
  const first = (await response.json()).data
  await waitForOperation()
  assert.deepEqual(selections[0], selections[1], "Reauthentication changed the selected package")
  assert.equal(selections[0].version, "1.0.0")
  assert.equal(selections[0].package_sha256, first.archive_digest)
  assert.equal((await jsonAPI("/themes/active")).built_in, true)
  await tab("Installed")
  await activate(first)
  return first
}

async function installCurrent() {
  const installed = page.waitForResponse((response) => response.url().endsWith("/install"))
  await sourceCard().getByRole("button", { name: "Install", exact: true }).click()
  const response = await installed
  assert.equal(response.status(), 201)
  await waitForOperation()
  return (await response.json()).data
}

async function activate(pkg) {
  await packageCard(pkg.version).getByRole("button", { name: "Activate", exact: true }).click()
  const changed = page.waitForResponse((response) => response.url().endsWith(`/themes/${pkg.id}/activate`))
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal((await changed).status(), 200)
  await waitForOperation()
  assert.equal((await jsonAPI("/themes/active")).package_id, pkg.id)
}

async function configureSecond(second) {
  await packageCard(second.version).getByRole("button", { name: "Preview", exact: true }).click()
  await page.frameLocator('iframe[title="Market Slate preview"]').getByRole("link", { name: "Frankfurt edge" }).waitFor()
  await page.getByLabel("Corner radius (px)").fill("0")
  await page.getByRole("button", { name: "Save settings", exact: true }).click()
  const saved = page.waitForResponse((response) => response.url().endsWith(`/themes/${second.id}/configuration`) && response.request().method() === "PUT")
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal((await saved).status(), 200)
  await page.getByRole("dialog", { name: "Save settings", exact: true }).waitFor({ state: "hidden" })
  await page.getByRole("dialog", { name: "Market Slate · Preview", exact: true }).getByRole("button", { name: "Close", exact: true }).click()
}

async function upgradeAndRestore(first) {
  await control("/catalog/v2")
  await tab("Market")
  const next = await refresh()
  assert.equal(next.themes[0].version, "2.0.0")
  const second = await installCurrent()
  assert.equal((await jsonAPI("/themes/active")).package_id, first.id, "Installation activated the new version")
  await tab("Installed")
  await configureSecond(second)
  await activate(second)
  await screenshot("market-installed-versions.png")
  await page.getByRole("button", { name: "Roll back", exact: true }).click()
  const rolledBack = page.waitForResponse((response) => response.url().endsWith("/themes/rollback"))
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal((await (await rolledBack).json()).data.rollback_target, first.id)
  await waitForOperation()
  await deletePackage(second, 204)
  assert.equal((await admin.request.get(`${base}/themes/assets/${second.archive_digest}/preview.png`)).status(), 404)
  await tab("Market")
  await refresh()
  const restored = await installCurrent()
  assert.equal(restored.id, second.id, "Reinstallation changed the package identity")
  assert.equal(JSON.parse(restored.configuration).radius, 0)
  assert.equal((await jsonAPI("/themes/active")).package_id, first.id, "Reinstallation activated the package")
  assert.equal((await jsonAPI(`/admin/themes/${restored.id}/preview`)).declarative.radius, 0)
  return restored
}

async function deletePackage(pkg, status) {
  await packageCard(pkg.version).getByRole("button", { name: "Delete", exact: true }).click()
  const deleted = page.waitForResponse((response) => response.url().endsWith(`/themes/${pkg.id}`) && response.request().method() === "DELETE")
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal((await deleted).status(), status)
  await waitForOperation()
}

async function captureMarketLayouts() {
  await screenshot("market-desktop.png")
  await page.evaluate(() => localStorage.setItem("theme", "dark"))
  await page.reload()
  await tab("Market")
  await refresh()
  await page.setViewportSize({ width: 390, height: 844 })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "Market overflows mobile")
  await screenshot("market-mobile-dark.png")
  await page.setViewportSize({ width: 1440, height: 1000 })
}

async function verifyPendingCleanup(second, first) {
  await control(`/cleanup/block/${second.id}`)
  await tab("Installed")
  await deletePackage(second, 202)
  await page.getByText(/The theme has been deleted and its assets are unavailable/).waitFor()
  assert.equal((await admin.request.get(`${base}/themes/assets/${second.archive_digest}/preview.png`)).status(), 404)
  await screenshot("market-cleanup-pending.png")
  await control(`/cleanup/unblock/${second.id}`)
  const cleaned = page.waitForResponse((response) => response.url().endsWith(`/themes/${second.id}`) && response.request().method() === "DELETE")
  await page.getByRole("button", { name: "Retry cleanup", exact: true }).click()
  assert.equal((await cleaned).status(), 204)
  await waitForOperation()
  assert.equal(await page.getByRole("button", { name: "Retry cleanup", exact: true }).count(), 0)
  assert.equal((await jsonAPI("/themes/active")).package_id, first.id)
}

async function removeSource() {
  await tab("Market")
  await sourceCard().getByRole("switch").click()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  await waitForOperation()
  assert.equal(await sourceCard().getByRole("button", { name: "Refresh catalog", exact: true }).isEnabled(), false)
  await sourceCard().getByRole("button", { name: "Delete", exact: true }).click()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  await waitForOperation()
  await page.getByText("No market sources configured.", { exact: true }).waitFor()
}
