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
const adminMarkerKeys = {
  workspace: "workspace.title",
  security: "security.title",
  settings: "settings.site",
  enrollment: "discovery.title",
  operations: "operations.title",
  lifecycle: "lifecycle.country_location_and_lifecycle",
  agentReleases: "agentRollout.title",
  accelerators: "accelerator.title",
  network: "network.manage",
  terminal: "terminal.title",
  groups: "group.manage",
  nodes: "node.manage_description",
}

/** 用 Vite 的 SSR 装载能力读取 TS 目录，并生成一份「词内膨胀 40%」的伪语言目录。 */
async function loadCatalogData() {
  const vite = await createServer({ root: webui, configFile: path.join(webui, "vite.config.ts"),
    server: { middlewareMode: true }, appType: "custom", logLevel: "error" })
  try {
    const { allCatalogs } = await vite.ssrLoadModule("/src/test/catalogs.ts")
    const { pseudoCatalog: build } = await vite.ssrLoadModule("/src/test/pseudo-locale.ts")
    const { localeOptions, translateFrom } = await vite.ssrLoadModule("/src/context/locales.ts")
    const english = {}
    for (const catalog of allCatalogs) Object.assign(english, catalog.en ?? {})
    const catalog = build(english)
    const hints = {}
    for (const option of localeOptions) {
      hints[option.code] = {
        language: translateFrom(allCatalogs, option.code, "app.language"),
        theme: translateFrom(allCatalogs, option.code, "app.theme"),
        fallback: translateFrom(allCatalogs, option.code, "app.not_found"),
        markers: Object.fromEntries(Object.entries(adminMarkerKeys).map(([name, key]) => [
          name, translateFrom(allCatalogs, option.code, key),
        ])),
        nativeName: option.nativeName,
      }
    }
    return { catalog, keys: Object.keys(catalog).length, hints }
  } finally {
    await vite.close()
  }
}

const { catalog, keys, hints: localeHints } = await loadCatalogData()
assert.ok(keys > 1000, `pseudo catalog is too small: ${keys} keys`)
const pseudoHints = {
  language: catalog["app.language"],
  theme: catalog["app.theme"],
  fallback: catalog["app.not_found"],
  markers: Object.fromEntries(Object.entries(adminMarkerKeys).map(([name, key]) => [name, catalog[key]])),
  nativeName: "日本語",
}
for (const key of ["language", "theme", "fallback"]) assert.ok(pseudoHints[key], `pseudo hint is missing: ${key}`)
for (const [name, value] of Object.entries(pseudoHints.markers)) assert.ok(value, `pseudo marker is missing: ${name}`)
const shard = `export const jaPublicCatalog = ${JSON.stringify({ ja: catalog })};\n` +
  `export const jaAdminCatalog = ${JSON.stringify({ ja: catalog })};\n`

const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })

const viewports = [{ name: "mobile", width: 390, height: 844 }, { name: "tablet", width: 768, height: 1024 }, { name: "desktop", width: 1440, height: 1000 }]
const publicPages = [["fleet", "/"], ["wallboard", "/wallboard"]]
const adminPages = [["metrics", "/admin/metrics", "workspace"], ["security", "/admin/security", "security"],
  ["settings", "/admin/settings", "settings"], ["enrollment", "/admin/enrollment", "enrollment"],
  ["operations", "/admin/operations", "operations"], ["lifecycle", "/admin/lifecycle", "lifecycle"],
  ["agent-releases", "/admin/agent-releases", "agentReleases"], ["accelerators", "/admin/accelerators", "accelerators"],
  ["network", "/admin/network", "network"], ["terminal", "/admin/terminal", "terminal"],
  ["groups", "/admin/groups", "groups"], ["nodes", "/admin/nodes", "nodes"]]

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

async function catalogSignals(page, hints, marker) {
  return page.evaluate(({ language, nativeName, theme, fallback, marker }) => {
    const attributes = [...document.querySelectorAll("[title], [aria-label]")]
      .flatMap((element) => [element.getAttribute("title"), element.getAttribute("aria-label")])
      .filter(Boolean)
    const bodyText = document.body.textContent || ""
    return {
      languageMenu: attributes.some((value) => value === language || (value.startsWith(`${language} · `) && value.includes(nativeName))),
      themeControl: attributes.includes(theme),
      fallback: bodyText.includes(fallback),
      marker: marker ? bodyText.includes(marker) : true,
    }
  }, { ...hints, marker })
}

function assertStrictVisit({ pass, name, viewport, status, text, leaked, measured, signals, pageErrors }) {
  const label = `${pass.mode} ${name} @${viewport.name}`
  check(`${label} loads`, status === 200, `status=${status}`)
  if (pass.kind === "pseudo") check(`${label} renders the pseudo catalog`, text.includes("⟦"), text.slice(0, 120))
  check(`${label} loads the selected locale`, signals.languageMenu || signals.fallback, JSON.stringify(pass.hints))
  check(`${label} localizes shared chrome`, signals.themeControl || signals.fallback, JSON.stringify(pass.hints))
  if (pass.marker) check(`${label} renders localized page content`, signals.marker, `${pass.marker}: ${pass.hints.markers[pass.marker]}`)
  check(`${label} has no raw catalog keys`, leaked.length === 0, leaked.slice(0, 6).join(","))
  check(`${label} keeps the page inside the viewport`, !measured.overflow,
    `scrollWidth=${measured.scrollWidth} clientWidth=${measured.clientWidth}`)
  check(`${label} has no page errors`, pageErrors.length === 0, pageErrors.slice(0, 3).join(" | "))
}

async function visit(page, name, url, viewport, pass, marker) {
  const errorStart = errors.length
  const pagePass = { ...pass, marker }
  await page.setViewportSize({ width: viewport.width, height: viewport.height })
  const response = await page.goto(base + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(1200)
  // 语言分片可能在首屏之后才注册，等到伪语言文案真的出现再测量。
  if (pagePass.kind === "pseudo") {
    const renderPseudo = async () => page.waitForFunction(() => document.body.innerText.includes("\u27e6"), null, { timeout: 12000 })
    try { await renderPseudo() } catch { await page.reload({ waitUntil: "domcontentloaded" }); await page.waitForTimeout(1200); await renderPseudo().catch(() => undefined) }
  }
  const status = response ? response.status() : 0
  const text = await page.locator("body").innerText()
  const leaked = [...new Set([...text.matchAll(rawKey)].map((match) => match[1]))]
  const measured = await measure(page, `${name}@${viewport.name}`)
  measured.overflow = measured.scrollWidth > measured.clientWidth + 1
  const signals = await catalogSignals(page, pagePass.hints, marker ? pagePass.hints.markers[marker] : undefined)
  const pageErrors = errors.slice(errorStart)
  if (leakMode) {
    const tokens = englishLeakTokens(text)
    if (tokens.length > 0) console.log(`LEAK ${pagePass.mode} ${name} @${viewport.name}: ${tokens.join(", ")}`)
    leakRecords.push({ mode: pagePass.mode, page: name, viewport: viewport.name, tokens })
  }
  if (pagePass.kind !== "english") {
    assertStrictVisit({ pass: pagePass, name, viewport, status, text, leaked, measured, signals, pageErrors })
  } else if (measured.overflow) {
    console.log(`INFO baseline ${name} @${viewport.name} already overflows: scrollWidth=${measured.scrollWidth} clientWidth=${measured.clientWidth}`)
  }
  if (pagePass.kind !== "english") {
    if (measured.clipped.length > 0) console.log(`INFO ${name} @${viewport.name} clipped elements: ${JSON.stringify(measured.clipped.slice(0, 4))}`)
  }
  records.push({ mode: pagePass.mode, page: name, viewport: viewport.name, status, leaked, signals, pageErrors, ...measured })
  await page.screenshot({ path: path.join(output, `${pagePass.mode}-${name}-${viewport.name}.png`), fullPage: false })
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

async function login(context, pass) {
  if (!user || !password) return false
  const response = await context.request.post(`${base}/api/v1/auth/login`, { headers: { Origin: origin }, data: { username: user, password } })
  if (pass.kind !== "english") check(`${pass.mode} admin login`, response.status() === 200, `status=${response.status()}`)
  const reauthentication = await context.request.post(`${base}/api/v1/auth/reauthenticate`, { headers: { Origin: origin }, data: { password } })
  if (pass.kind !== "english") check(`${pass.mode} recent authentication`, reauthentication.ok(), `status=${reauthentication.status()}`)
  return response.status() === 200
}

async function runPass(pass) {
  const context = await browser.newContext({ viewport: { width: viewports[2].width, height: viewports[2].height } })
  await context.addInitScript((code) => {
    // 主题沙箱 iframe 与冻结文档里访问 localStorage 会抛 SecurityError，这里静默跳过。
    try { localStorage.setItem("locale", code); localStorage.setItem("theme", "light") } catch { /* 沙箱文档没有存储 */ }
  }, pass.locale)
  if (pass.intercept) await context.route(/\/assets\/[^/]*ja-[^/]*\.js$/, (route) => route.fulfill({ status: 200, contentType: "application/javascript", body: shard }))
  context.on("weberror", (error) => errors.push(`${pass.mode}: ${error.error().message}`))
  const page = await context.newPage()
  page.on("pageerror", (error) => errors.push(`${pass.mode}: ${error.message}`))
  try {
    const authorised = await login(context, pass)
    if (!authorised && pass.kind !== "english") console.log("INFO no admin session: skipping the admin pages")
    const targets = authorised ? [...pages, ...adminPages] : pages
    for (const [name, url, marker] of targets) {
      for (const viewport of viewports) await visit(page, name, url, viewport, pass, marker)
    }
    await page.setViewportSize({ width: 768, height: 1024 })
    await page.goto(base, { waitUntil: "domcontentloaded" })
    await page.waitForTimeout(1200)
    const detail = page.locator('a[href^="/node/"]').first()
    const linked = await detail.count() > 0
    if (pass.kind !== "english") check(`${pass.mode} public fleet links a node detail page`, linked)
    if (linked) {
      const href = await detail.getAttribute("href")
      for (const viewport of viewports) await visit(page, "node-detail", href, viewport, pass)
    }
  } finally {
    await context.close()
  }
}

try {
  if (process.env.BEAT_SMOKE_PSEUDO_ONLY !== "1") {
    await runPass({ kind: "english", mode: "english", locale: "en", hints: localeHints.en })
    for (const [locale, mode] of [["zh-CN", "locale-zh-CN"], ["zh-TW", "locale-zh-TW"], ["ja", "locale-ja"], ["id", "locale-id"]]) {
      await runPass({ kind: "locale", mode, locale, hints: localeHints[locale] })
    }
  }
  await runPass({ kind: "pseudo", mode: "pseudo", locale: "ja", hints: pseudoHints, intercept: true })
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
