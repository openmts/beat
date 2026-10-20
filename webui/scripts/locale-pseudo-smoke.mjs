import assert from "node:assert/strict"
import { mkdir, writeFile } from "node:fs/promises"
import path from "node:path"
import { createServer } from "vite"

const [base, output] = process.argv.slice(2)
if (!base || !output) throw new Error("usage: node locale-pseudo-smoke.mjs BASE_URL OUTPUT_DIR")
const webui = process.env.BEAT_WEBUI_ROOT || "/root/projects/beat/webui"
const user = process.env.BEAT_SMOKE_USER
const password = process.env.BEAT_SMOKE_PASSWORD
// 预览最新构建产物时入口端口与服务端放通来源不同，这里允许显式指定登录用的 Origin。
const origin = process.env.BEAT_SMOKE_ORIGIN || base

/** 用 Vite 的 SSR 装载能力读取 TS 目录，生成一份「词内膨胀 40%」的伪语言目录。 */
async function pseudoCatalog() {
  const vite = await createServer({ root: webui, configFile: path.join(webui, "vite.config.ts"),
    server: { middlewareMode: true }, appType: "custom", logLevel: "error" })
  try {
    const { allCatalogs } = await vite.ssrLoadModule("/src/test/catalogs.ts")
    const { pseudoCatalog: build } = await vite.ssrLoadModule("/src/test/pseudo-locale.ts")
    const english = {}
    for (const catalog of allCatalogs) Object.assign(english, catalog.en ?? {})
    const catalog = build(english)
    return { catalog, keys: Object.keys(catalog).length }
  } finally {
    await vite.close()
  }
}

const { catalog, keys } = await pseudoCatalog()
assert.ok(keys > 1000, `pseudo catalog is too small: ${keys} keys`)
const shard = `export const jaPublicCatalog = ${JSON.stringify({ ja: catalog })};\n` +
  `export const jaAdminCatalog = ${JSON.stringify({ ja: catalog })};\n`

const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })

const viewports = [{ name: "mobile", width: 390, height: 844 }, { name: "tablet", width: 768, height: 1024 }, { name: "desktop", width: 1440, height: 1000 }]
const publicPages = [["fleet", "/"], ["wallboard", "/wallboard"]]
const adminPages = [["metrics", "/admin/metrics"], ["security", "/admin/security"], ["settings", "/admin/settings"],
  ["enrollment", "/admin/enrollment"], ["operations", "/admin/operations"], ["lifecycle", "/admin/lifecycle"],
  ["agent-releases", "/admin/agent-releases"], ["accelerators", "/admin/accelerators"], ["network", "/admin/network"],
  ["backup", "/admin/backup"], ["themes", "/admin/themes"], ["nodes", "/admin/nodes"]]

/** 目录键泄漏成界面文本的样子：`app.nodes`、`workspace.stats_totals` 这类点分小写标识。 */
const rawKey = /(?:^|[^A-Za-z0-9_.])((?:app|nav|admin|theme|workspace|fleet|node|metric|metrics|enrollment|security|backup|lifecycle|notification|operation|secret|diagnostic|diagnostics|traffic|telemetry|maintenance|migration|ingress|evaluation|accelerator|authentication|billing)\.[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)/g


const records = []
const failures = []
let checks = 0

function check(name, ok, detail = "") {
  checks += 1
  if (!ok) failures.push(`${name}${detail ? ` :: ${detail}` : ""}`)
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${ok || !detail ? "" : ` :: ${detail}`}`)
}

async function measure(page, label) {
  return page.evaluate((tag) => {
    const root = document.documentElement
    const clipped = []
    for (const element of document.querySelectorAll("main *, header *, aside *")) {
      const style = getComputedStyle(element)
      if (style.overflowX !== "hidden" && style.overflowX !== "clip" && style.textOverflow !== "ellipsis") continue
      if (element.scrollWidth > element.clientWidth + 4 && element.clientWidth > 0) {
        clipped.push({ tag: element.tagName.toLowerCase(), text: (element.textContent || "").trim().slice(0, 60) })
      }
    }
    return { label: tag, scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, clipped: clipped.slice(0, 20) }
  }, label)
}

async function visit(page, name, url, viewport, mode) {
  await page.setViewportSize({ width: viewport.width, height: viewport.height })
  const response = await page.goto(base + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(1200)
  // 语言分片可能在首屏之后才注册，等到伪语言文案真的出现再测量。
  if (mode === "pseudo") {
    const renderPseudo = async () => page.waitForFunction(() => document.body.innerText.includes("\u27e6"), null, { timeout: 12000 })
    try { await renderPseudo() } catch { await page.reload({ waitUntil: "domcontentloaded" }); await page.waitForTimeout(1200); await renderPseudo().catch(() => undefined) }
  }
  const status = response ? response.status() : 0
  const text = await page.locator("body").innerText()
  const leaked = [...new Set([...text.matchAll(rawKey)].map((match) => match[1]))]
  const measured = await measure(page, `${name}@${viewport.name}`)
  const overflow = measured.scrollWidth > measured.clientWidth + 1
  if (leakMode) {
    const tokens = englishLeakTokens(text)
    if (tokens.length > 0) console.log(`LEAK ${mode} ${name} @${viewport.name}: ${tokens.join(", ")}`)
    leakRecords.push({ mode, page: name, viewport: viewport.name, tokens })
  }
  if (mode === "pseudo") {
    check(`${name} @${viewport.name} loads`, status === 200, `status=${status}`)
    check(`${name} @${viewport.name} renders the pseudo catalog`, text.includes("⟦"), text.slice(0, 120))
    check(`${name} @${viewport.name} has no raw catalog keys`, leaked.length === 0, leaked.slice(0, 6).join(","))
    check(`${name} @${viewport.name} keeps the page inside the viewport`, !overflow,
      `scrollWidth=${measured.scrollWidth} clientWidth=${measured.clientWidth}`)
    if (measured.clipped.length > 0) console.log(`INFO ${name} @${viewport.name} clipped elements: ${JSON.stringify(measured.clipped.slice(0, 4))}`)
  } else if (overflow) {
    console.log(`INFO baseline ${name} @${viewport.name} already overflows: scrollWidth=${measured.scrollWidth} clientWidth=${measured.clientWidth}`)
  }
  records.push({ mode, page: name, viewport: viewport.name, status, leaked, ...measured })
  await page.screenshot({ path: path.join(output, `${mode}-${name}-${viewport.name}.png`), fullPage: false })
}

/** 硬编码英文候选：伪语言把目录文案整条包进 ⟦⟧，所以界定符之外剩下的英文单词只能来自代码字面量或数据。 */
const leakMode = process.env.BEAT_SMOKE_ENGLISH_LEAKS === "1"
const leakAllow = new Set(["Beat", "CPU", "RAM", "GPU", "SSH", "HTTP", "HTTPS", "API", "JSON", "CSV", "JSONL", "UTC", "TLS", "DNS", "IP", "KiB", "MiB", "GiB", "Linux", "Docker", "OK", "ID", "URL", "PWA", "MTS", "SHA", "Ed25519", "Sigstore", "GeoIP", "HTMX"])
function englishLeakTokens(text) {
  const outside = text.replace(/⟦[^⟧]*⟧/g, " ")
  const tokens = [...outside.matchAll(/[A-Za-z][A-Za-z'’-]+/g)].map((match) => match[0].replace(/[’'-]+$/, ""))
  return [...new Set(tokens)].filter((token) => {
    if (leakAllow.has(token) || token.length < 4) return false
    if (/[0-9_]/.test(token) || /^[A-Z]+$/.test(token)) return false
    return true
  })
}

const errors = []
const leakRecords = []
const pages = [...publicPages]

async function login(context, mode) {
  if (!user || !password) return false
  const response = await context.request.post(`${base}/api/v1/auth/login`, { headers: { Origin: origin }, data: { username: user, password } })
  if (mode === "pseudo") check("admin login", response.status() === 200, `status=${response.status()}`)
  await context.request.post(`${base}/api/v1/auth/reauthenticate`, { headers: { Origin: origin }, data: { password } })
  return response.status() === 200
}

async function runPass(mode, locale, intercept) {
  const context = await browser.newContext({ viewport: { width: viewports[2].width, height: viewports[2].height } })
  await context.addInitScript((code) => {
    // 主题沙箱 iframe 与冻结文档里访问 localStorage 会抛 SecurityError，这里静默跳过。
    try { localStorage.setItem("locale", code); localStorage.setItem("theme", "light") } catch { /* 沙箱文档没有存储 */ }
  }, locale)
  if (intercept) await context.route(/\/assets\/[^/]*ja-[^/]*\.js$/, (route) => route.fulfill({ status: 200, contentType: "application/javascript", body: shard }))
  context.on("weberror", (error) => errors.push(`${mode}: ${error.error().message}`))
  const page = await context.newPage()
  page.on("pageerror", (error) => errors.push(`${mode}: ${error.message}`))
  const authorised = await login(context, mode)
  if (!authorised && mode === "pseudo") console.log("INFO no admin session: skipping the admin pages")
  const targets = authorised ? [...pages, ...adminPages] : pages
  for (const [name, url] of targets) {
    for (const viewport of viewports) await visit(page, name, url, viewport, mode)
  }
  await page.setViewportSize({ width: 768, height: 1024 })
  await page.goto(base, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(1200)
  const detail = page.locator('a[href^="/node/"]').first()
  const linked = await detail.count() > 0
  if (mode === "pseudo") check("public fleet links a node detail page", linked)
  if (linked) {
    const href = await detail.getAttribute("href")
    for (const viewport of viewports) await visit(page, "node-detail", href, viewport, mode)
  }
  await context.close()
}

try {
  if (process.env.BEAT_SMOKE_PSEUDO_ONLY !== "1") await runPass("english", "en", false)
  await runPass("pseudo", "ja", true)
  check("no page errors during the passes", errors.length === 0, errors.slice(0, 3).join(" | "))
} finally {
  await browser.close()
}

if (leakMode) {
  const pseudo = leakRecords.filter((record) => record.mode === "pseudo" && record.tokens.length > 0)
  const byToken = new Map()
  for (const record of pseudo) for (const token of record.tokens) byToken.set(token, (byToken.get(token) || 0) + 1)
  console.log(`LEAK-SUMMARY pages=${new Set(pseudo.map((record) => record.page)).size} tokens=${byToken.size}`)
  console.log(`LEAK-TOKENS ${[...byToken.entries()].sort((a, b) => b[1] - a[1]).map(([token, count]) => `${token}×${count}`).join(", ")}`)
}
await writeFile(path.join(output, "results.json"), `${JSON.stringify({ checks, failures, records, leaks: leakRecords }, null, 2)}\n`, { mode: 0o600 })
console.log(`${checks - failures.length}/${checks} assertions passed; ${records.length} page/viewport samples`)
if (failures.length > 0) {
  console.log(`failures:\n - ${failures.join("\n - ")}`)
  process.exitCode = 1
}
