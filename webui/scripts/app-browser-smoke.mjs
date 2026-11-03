import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { createHmac } from "node:crypto"
import { chmod, mkdir, stat, unlink, writeFile } from "node:fs/promises"
import path from "node:path"

const [base, output] = process.argv.slice(2)
if (!base || !output) throw new Error("usage: node app-browser-smoke.mjs SERVER_URL OUTPUT_DIR")

const bootstrapToken = process.env.BEAT_E2E_BOOTSTRAP_TOKEN
const username = process.env.BEAT_E2E_USERNAME
const password = process.env.BEAT_E2E_PASSWORD
if (!bootstrapToken || !username || !password) throw new Error("browser smoke credentials are required")
const sshHost = process.env.BEAT_E2E_SSH_HOST?.trim() ?? ""
const sshPort = Number(process.env.BEAT_E2E_SSH_PORT ?? "0")
const sshAuthorizedKeys = process.env.BEAT_E2E_SSH_AUTHORIZED_KEYS?.trim() ?? ""
const terminalEnabled = sshHost !== "" || sshAuthorizedKeys !== "" || sshPort > 0
if (terminalEnabled && (!sshHost || !Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535 || !sshAuthorizedKeys)) {
  throw new Error("complete terminal smoke SSH environment is required")
}

const { chromium } = await import(process.env.BEAT_PLAYWRIGHT_MODULE || "playwright")
await mkdir(output, { recursive: true, mode: 0o700 })
const browser = await chromium.launch({ headless: true, executablePath: process.env.BEAT_BROWSER_EXECUTABLE })
const context = await browser.newContext({ viewport: { width: 1280, height: 960 }, locale: "en-US" })
await context.addInitScript(() => {
  localStorage.setItem("locale", "en")
  localStorage.setItem("theme", "light")
})
const page = await context.newPage()
const pageErrors = []
const serverErrors = []
let agentProcess = null
let agentConfigPath = ""
let agentOutput = ""
let agentFailure = ""
page.on("pageerror", error => pageErrors.push(error.message))
page.on("response", response => {
  if (response.status() >= 500) serverErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`)
})

let currentStep = "public page"
try {
  const publicResponse = await page.goto(`${base}/`, { waitUntil: "domcontentloaded" })
  assert.equal(publicResponse?.status(), 200)
  await page.getByRole("heading", { name: "Network quality", exact: true }).waitFor()
  await screenshot("public-home.png")

  currentStep = "owner bootstrap"
  await page.goto(`${base}/admin`, { waitUntil: "domcontentloaded" })
  await page.getByText("Create the owner account", { exact: true }).waitFor()
  await page.locator("#bootstrap-token").fill(bootstrapToken)
  await page.locator("#admin-username").fill(username)
  await page.locator("#display-name").fill("Browser Owner")
  await page.locator("#admin-password").fill(password)
  const bootstrap = page.waitForResponse(response => response.url().endsWith("/api/v1/auth/bootstrap"))
  await page.getByRole("button", { name: "Create owner", exact: true }).click()
  assert.equal((await bootstrap).status(), 201)
  await page.getByRole("link", { name: "Site settings", exact: true }).waitFor()

  currentStep = "outbound proxy settings"
  await page.goto(`${base}/admin/settings`, { waitUntil: "domcontentloaded" })
  await page.getByText("Site settings", { exact: true }).first().waitFor()
  const proxyCard = page.locator('[data-slot="card"]').filter({ hasText: "Outbound proxy" })
  await proxyCard.waitFor()
  assert.match(await proxyCard.innerText(), /Disabled/)
  await page.getByRole("switch", { name: "Use outbound proxy", exact: true }).click()
  await page.locator("#egress-proxy-url").fill("http://127.0.0.1:7890")
  await page.locator("#egress-proxy-username").fill("browser-owner")
  await page.locator("#egress-proxy-password").fill("proxy-password-must-not-leak")
  await page.getByRole("button", { name: "Save proxy settings", exact: true }).click()
  const confirmation = page.getByRole("dialog")
  await confirmation.waitFor()
  const proof = confirmation.getByLabel("Current password")
  if (await proof.isVisible()) await proof.fill(password)
  const save = page.waitForResponse(response => response.url().endsWith("/api/v1/admin/settings/egress-proxy") && response.request().method() === "PUT")
  await confirmation.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal((await save).status(), 200)
  await page.getByText("Outbound proxy settings saved", { exact: true }).waitFor()
  await page.getByText("Configuration revision 2", { exact: true }).waitFor()

  currentStep = "proxy persistence and secrecy"
  await page.reload({ waitUntil: "domcontentloaded" })
  await page.getByText("Configuration revision 2", { exact: true }).waitFor()
  assert.equal(await page.locator("#egress-proxy-url").inputValue(), "http://127.0.0.1:7890")
  await page.getByText(/A password is saved/).waitFor()
  assert.equal(await page.locator("#egress-proxy-password").inputValue(), "")
  const settings = await page.evaluate(async () => {
    const response = await fetch("/api/v1/admin/settings/egress-proxy", { credentials: "include" })
    return { status: response.status, body: await response.text() }
  })
  assert.equal(settings.status, 200)
  assert.match(settings.body, /"has_password":true/)
  assert.doesNotMatch(settings.body, /proxy-password-must-not-leak/)
  assert.doesNotMatch(settings.body, /"password":/)
  const sessionCookie = (await context.cookies(base)).find(cookie => cookie.name === "beat_admin_session")
  assert.ok(sessionCookie, "administrator session cookie missing")
  assert.equal(sessionCookie.httpOnly, true)
  assert.equal(sessionCookie.sameSite, "Strict")
  await screenshot("proxy-settings.png")

  let terminalKey = null
  if (terminalEnabled) {
    currentStep = "terminal ssh key"
    const generated = await page.evaluate(async () => {
      const response = await fetch("/api/v1/ssh-keys/generate", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: "browser-terminal", key_type: "ed25519" }),
      })
      return { status: response.status, body: await response.text() }
    })
    assert.equal(generated.status, 201)
    terminalKey = JSON.parse(generated.body)?.data
    assert.equal(terminalKey?.name, "browser-terminal")
    assert.equal(terminalKey?.key_type, "ed25519")
    assert.match(terminalKey?.private_key ?? "", /BEGIN OPENSSH PRIVATE KEY/)
    await writeFile(sshAuthorizedKeys, `${terminalKey.public_key}\n`, { mode: 0o600 })
    await chmod(sshAuthorizedKeys, 0o600)
  }

  currentStep = "node creation"
  await page.goto(`${base}/admin/nodes`, { waitUntil: "domcontentloaded" })
  await page.getByRole("heading", { name: "Nodes", exact: true }).waitFor()
  await page.getByRole("button", { name: "Create node", exact: true }).click()
  const createDialog = page.getByRole("dialog")
  await createDialog.getByLabel("Name", { exact: true }).fill("browser-node")
  await createDialog.getByLabel("Host", { exact: true }).fill(terminalEnabled ? sshHost : "192.0.2.10")
  await createDialog.getByLabel("Port", { exact: true }).fill(terminalEnabled ? String(sshPort) : "22")
  if (terminalKey) {
    await createDialog.getByRole("combobox", { name: "SSH Key", exact: true }).click()
    await page.getByRole("option", { name: terminalKey.name, exact: true }).click()
  }
  const createRequest = page.waitForResponse(response => response.url().endsWith("/api/v1/nodes") && response.request().method() === "POST")
  await createDialog.getByRole("button", { name: "Create", exact: true }).click()
  const createConfirmation = page.getByRole("dialog")
  await createConfirmation.getByRole("button", { name: "Confirm", exact: true }).click()
  const created = await createRequest
  assert.equal(created.status(), 201)
  const envelope = await created.json()
  const credential = envelope?.data
  assert.equal(typeof credential?.agent_token, "string", `unexpected create response keys: ${Object.keys(envelope ?? {}).join(",")}`)
  assert.match(credential.agent_token, /^beat_agent_v1_/)
  assert.equal(credential.agent_config.node_name, "browser-node")
  await page.getByText(credential.agent_token, { exact: true }).waitFor()
  await page.getByRole("button", { name: "Confirm", exact: true }).click()
  assert.equal(await page.locator(`[data-testid="node-card-${credential.node.id}"]`).count(), 1)
  const managed = await page.evaluate(async () => {
    const response = await fetch("/api/v1/nodes/manage", { credentials: "include" })
    return { status: response.status, body: await response.text() }
  })
  assert.equal(managed.status, 200)
  assert.match(managed.body, /browser-node/)
  assert.doesNotMatch(managed.body, new RegExp(credential.agent_token))
  assert.doesNotMatch(managed.body, /agent_token_hash/)
  if (process.env.BEAT_APP_BROWSER_AGENT) {
    currentStep = "agent reporting"
    agentConfigPath = path.join(output, "agent-config.json")
    await writeFile(agentConfigPath, JSON.stringify(credential.agent_config), { mode: 0o600, flag: "wx" })
    await chmod(agentConfigPath, 0o600)
    agentProcess = spawn(process.env.BEAT_APP_BROWSER_AGENT, ["--config", agentConfigPath], {
      cwd: output,
      env: { ...process.env, HOME: output },
      stdio: ["ignore", "pipe", "pipe"],
    })
    agentProcess.once("error", error => { agentFailure = error.message })
    for (const stream of [agentProcess.stdout, agentProcess.stderr]) {
      stream.setEncoding("utf8")
      stream.on("data", chunk => { agentOutput = (agentOutput + chunk).slice(-8_192) })
    }
    await waitForManagedNode(credential.node.id)
  }

  currentStep = "public node visibility"
  await waitForPublicNode("browser-node", true)
  await screenshot("public-node.png")

  if (terminalEnabled) {
    currentStep = "terminal SSH and batch command"
    await exerciseTerminal(credential.node.id)
  }

  currentStep = "private node visibility"
  await page.goto(`${base}/admin/nodes`, { waitUntil: "domcontentloaded" })
  const nodeCard = page.getByTestId(`node-card-${credential.node.id}`)
  await nodeCard.waitFor()
  await nodeCard.getByRole("button", { name: "Edit", exact: true }).click()
  const editDialog = page.getByRole("dialog")
  const visibility = editDialog.getByRole("switch", { name: "Public visibility", exact: true })
  assert.equal(await visibility.isChecked(), true)
  await visibility.click()
  const updateRequest = page.waitForResponse(response => response.url().endsWith(`/api/v1/nodes/${credential.node.id}`) && response.request().method() === "PUT")
  await editDialog.getByRole("button", { name: "Save", exact: true }).click()
  assert.equal((await updateRequest).status(), 200)
  await waitForPublicNode("browser-node", false)

  currentStep = "backup download and staged restore"
  await exerciseBackup()

  currentStep = "authenticator enrollment"
  const totpSecret = await exerciseTOTP()

  currentStep = "logout and login"
  const logoutStatus = await page.evaluate(async () => (await fetch("/api/v1/auth/logout", { method: "POST", credentials: "include" })).status)
  assert.equal(logoutStatus, 200)
  assert.equal((await context.cookies(base)).some(cookie => cookie.name === "beat_admin_session"), false)
  await page.goto(`${base}/admin`, { waitUntil: "domcontentloaded" })
  await page.getByText(/Sign in with your administrator account\./).waitFor()
  await page.locator("#admin-username").fill(username)
  await page.locator("#admin-password").fill(password)
  const challenge = page.waitForResponse(response => response.url().endsWith("/api/v1/auth/login"))
  await page.getByRole("button", { name: "Open admin", exact: true }).click()
  assert.equal((await challenge).status(), 428)
  await page.locator("#totp-code").fill(totp(totpSecret))
  const login = page.waitForResponse(response => response.url().endsWith("/api/v1/auth/login"))
  await page.getByRole("button", { name: "Open admin", exact: true }).click()
  assert.equal((await login).status(), 200)
  await page.getByRole("link", { name: "Site settings", exact: true }).waitFor()

  assert.deepEqual(pageErrors, [], `browser page errors: ${pageErrors.join("; ")}`)
  assert.deepEqual(serverErrors, [], `server error responses: ${serverErrors.join("; ")}`)
  console.log(`app-browser-smoke: PASS (${output})`)
} catch (error) {
  const body = await page.locator("body").innerText().catch(() => "")
  await screenshot("failure.png").catch(() => {})
  throw new Error(`${currentStep}: ${error instanceof Error ? error.message : String(error)}\nbody: ${body.slice(0, 1200)}`)
} finally {
  await stopAgent()
  if (agentConfigPath) await unlink(agentConfigPath).catch(() => {})
  await browser.close()
}

async function screenshot(name) {
  const destination = path.join(output, name)
  await page.screenshot({ path: destination, fullPage: true })
  await chmod(destination, 0o600)
}

// 公共快照按 5 秒生产周期整体替换；每次重新加载文档，避免把上一代 DOM 当作最新结果。
async function waitForPublicNode(name, expected) {
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const nodesResponse = page.waitForResponse(response => response.url().endsWith("/api/v1/nodes") && response.request().method() === "GET")
    await page.goto(`${base}/`, { waitUntil: "domcontentloaded" })
    const response = await nodesResponse
    assert.equal(response.status(), 200)
    if ((await response.text()).includes(name) === expected) {
      try {
        if (expected) await page.getByText(name, { exact: true }).waitFor({ timeout: 5_000 })
        else {
          await page.getByText("No matching public nodes", { exact: true }).waitFor({ timeout: 5_000 })
          assert.equal(await page.getByText(name, { exact: true }).count(), 0)
        }
        return
      } catch {}
    }
    await page.waitForTimeout(500)
  }
  throw new Error(`public node ${name} did not become ${expected ? "visible" : "hidden"}`)
}

async function exerciseTerminal(nodeId) {
  await page.goto(`${base}/admin/terminal`, { waitUntil: "domcontentloaded" })
  const selector = page.getByRole("combobox", { name: "Select Node", exact: true })
  await selector.waitFor()
  await selector.click()
  await page.getByRole("option", { name: /browser-node/ }).click()

  const socketPromise = page.waitForEvent("websocket", socket => socket.url().includes(`/api/v1/ws/terminal?node_id=${nodeId}`))
  await page.getByRole("button", { name: "Connect", exact: true }).click()
  const socket = await socketPromise
  await page.getByRole("button", { name: "Disconnect", exact: true }).waitFor()

  const marker = "BEAT_TERMINAL_UI_OK"
  let received = ""
  socket.on("framereceived", frame => { received += frame.payload.toString() })
  await page.locator(".xterm-helper-textarea").focus()
  await page.keyboard.type(`printf '${marker}\\n'`)
  await page.keyboard.press("Enter")
  const terminalDeadline = Date.now() + 15_000
  while (!received.includes(marker) && Date.now() < terminalDeadline) await page.waitForTimeout(100)
  assert.match(received, new RegExp(marker), `terminal output did not contain ${marker}`)
  await screenshot("terminal-connected.png")
  await page.getByRole("button", { name: "Disconnect", exact: true }).click()

  const batchMarker = "BEAT_TERMINAL_BATCH_OK"
  const command = `printf '${batchMarker}\\n'`
  await page.getByPlaceholder("e.g. uptime").fill(command)
  const batch = page.waitForResponse(response => response.url().endsWith("/api/v1/terminal/execute") && response.request().method() === "POST")
  await page.getByRole("button", { name: "Execute on all online nodes", exact: true }).click()
  const response = await batch
  assert.equal(response.status(), 200)
  const results = (await response.json())?.data ?? []
  assert.equal(results.length, 1)
  assert.equal(results[0].node_id, nodeId)
  assert.equal(results[0].error, undefined)
  assert.match(results[0].output ?? "", new RegExp(batchMarker))
  await page.locator("pre").filter({ hasText: batchMarker }).waitFor()
  await screenshot("terminal-batch.png")
}

async function exerciseBackup() {
  await page.goto(`${base}/admin/security`, { waitUntil: "domcontentloaded" })
  await page.getByRole("tab", { name: "Backup and restore", exact: true }).click()
  const create = page.waitForResponse(response => response.url().endsWith("/api/v1/admin/backups") && response.request().method() === "POST")
  await page.getByRole("button", { name: "Create backup", exact: true }).click()
  const created = await create
  assert.equal(created.status(), 202)
  const record = (await created.json()).data
  assert.equal(typeof record?.id, "string")
  const card = page.locator('[data-slot="card"]').filter({ hasText: record.filename })
  await card.waitFor()
  await card.getByText("Ready", { exact: true }).waitFor({ timeout: 30_000 })

  const destination = path.join(output, "backup.zip")
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    card.getByRole("button", { name: "Download", exact: true }).click(),
  ])
  await download.saveAs(destination)
  await chmod(destination, 0o600)
  assert.ok((await stat(destination)).size > 1024, "downloaded backup archive is unexpectedly small")

  await card.getByRole("button", { name: "Restore", exact: true }).click()
  const restoreDialog = page.getByRole("dialog")
  await restoreDialog.getByLabel("Type RESTORE BEAT to confirm", { exact: true }).fill("RESTORE BEAT")
  const stage = page.waitForResponse(response => response.url().endsWith(`/api/v1/admin/backups/${record.id}/stage-restore`) && response.request().method() === "POST")
  await restoreDialog.getByRole("button", { name: "Apply on restart", exact: true }).click()
  assert.equal((await stage).status(), 200)
  await page.getByText("Restore is staged. Restart the Beat server to apply it.", { exact: true }).waitFor()
  await screenshot("backup-ready.png")
}

async function exerciseTOTP() {
  await page.goto(`${base}/admin/security`, { waitUntil: "domcontentloaded" })
  await page.getByRole("tab", { name: "Two-factor", exact: true }).click()
  await page.getByRole("button", { name: "Set up authenticator", exact: true }).click()
  await page.locator("#totp-current-password").fill(password)
  const begin = page.waitForResponse(response => response.url().endsWith("/api/v1/admin/users/me/totp") && response.request().method() === "POST")
  await page.getByRole("button", { name: "Confirm identity", exact: true }).click()
  const setupResponse = await begin
  assert.equal(setupResponse.status(), 200)
  const secret = (await setupResponse.json()).data?.secret
  assert.match(secret, /^[A-Z2-7]+=*$/)

  await page.locator("#enable-totp").fill(totp(secret))
  const enable = page.waitForResponse(response => response.url().endsWith("/api/v1/admin/users/me/totp") && response.request().method() === "POST")
  await page.getByRole("button", { name: "Enable authenticator", exact: true }).click()
  assert.equal((await enable).status(), 200)
  // 身份刷新会让安全页回到默认标签；重新打开 Two-factor 验证成功状态。
  await page.getByRole("tab", { name: "Two-factor", exact: true }).click()
  await page.getByText("Authenticator verification is enabled.", { exact: true }).waitFor()
  return secret
}

function totp(secret, now = Date.now()) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
  const bytes = []
  let bits = 0
  let value = 0
  for (const char of secret.replace(/=+$/, "").toUpperCase()) {
    const index = alphabet.indexOf(char)
    if (index < 0) throw new Error("invalid TOTP secret")
    value = (value << 5) | index
    bits += 5
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff)
      bits -= 8
    }
  }
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(now / 30_000)))
  const digest = createHmac("sha1", Buffer.from(bytes)).update(counter).digest()
  const offset = digest[digest.length - 1] & 0x0f
  return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(6, "0")
}

async function waitForManagedNode(id) {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (agentFailure) {
      throw new Error(`agent failed to start: ${agentFailure}`)
    }
    if (agentProcess?.exitCode !== null && agentProcess?.exitCode !== undefined) {
      throw new Error(`agent exited with ${agentProcess.exitCode}: ${agentOutput}`)
    }
    if (agentProcess?.signalCode) {
      throw new Error(`agent exited with ${agentProcess.signalCode}: ${agentOutput}`)
    }
    const node = await page.evaluate(async nodeId => {
      const response = await fetch("/api/v1/nodes/manage", { credentials: "include", cache: "no-store" })
      if (!response.ok) throw new Error(`managed nodes returned ${response.status}`)
      const body = await response.json()
      return (body.data ?? []).find(candidate => candidate.id === nodeId) ?? null
    }, id)
    if (node?.status === "online" && node.telemetry_state === "fresh" && Object.keys(node.metrics ?? {}).length > 0) return
    await page.waitForTimeout(500)
  }
  throw new Error(`agent did not report fresh metrics: ${agentOutput}`)
}

async function stopAgent() {
  if (!agentProcess) return
  const processToStop = agentProcess
  if (processToStop.exitCode !== null || processToStop.signalCode !== null || !processToStop.pid) {
    agentProcess = null
    return
  }
  processToStop.kill("SIGTERM")
  await new Promise(resolve => {
    const timer = setTimeout(() => {
      processToStop.kill("SIGKILL")
      resolve()
    }, 5_000)
    processToStop.once("exit", () => {
      clearTimeout(timer)
      resolve()
    })
  })
  agentProcess = null
}
