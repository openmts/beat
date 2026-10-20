import assert from "node:assert/strict"
import test from "node:test"
import { connectBeatTheme } from "./bridge.js"

function fixture(render = () => {}) {
  const listeners = new Set()
  const sent = []
  const parent = { postMessage: (value, origin) => sent.push({ value, origin }) }
  const scope = { parent, location: { href: "https://beat.example/themes/assets/hash/index.html" },
    addEventListener: (_, listener) => listeners.add(listener), removeEventListener: (_, listener) => listeners.delete(listener) }
  const client = connectBeatTheme(render, scope)
  const receive = (beat, options = {}) => { for (const listener of listeners) listener({ source: parent, origin: "https://beat.example", data: { beat }, ...options }) }
  const instance = "a".repeat(32)
  const init = () => receive({ version: 2, type: "init", instance_id: instance })
  const snapshot = (payload) => receive({ version: 2, type: "snapshot", instance_id: instance, payload })
  return { scope, client, sent, receive, init, snapshot, instance, listeners }
}

test("binds to the parent origin, version and fresh instance", () => {
  const values = []
  const f = fixture((snapshot) => values.push(snapshot))
  assert.equal(f.client.navigate("/"), false)
  f.receive({ version: 2, type: "init", instance_id: f.instance }, { origin: "https://evil.example" })
  f.receive({ version: 2, type: "init", instance_id: f.instance }, { source: {} })
  f.receive({ version: 1, type: "init", instance_id: f.instance })
  f.receive({ version: 2, type: "init", instance_id: "wrong" })
  f.receive(undefined)
  assert.equal(f.sent.length, 0)
  f.init()
  assert.deepEqual(f.sent[0], { value: { beat: { version: 2, instance_id: f.instance, type: "ready" } }, origin: "https://beat.example" })
  f.receive({ version: 2, type: "snapshot", instance_id: "b".repeat(32), payload: { nodes: [] } })
  assert.equal(values.length, 0)
  f.snapshot({ nodes: [{ id: "public-node" }], state: { nodes: "ready" } })
  assert.equal(values.length, 1)
  assert.equal(f.client.navigate("/node/public-node"), true)
  assert.equal(f.client.navigate("/"), true)
  for (const path of ["/admin", "//evil.example", "/node/hidden", "/node/public-node?x=1", "/node/public-node#x"]) assert.equal(f.client.navigate(path), false)
  f.init()
  assert.equal(f.client.navigate("/node/public-node"), false)
  f.client.destroy()
  f.client.destroy()
  assert.equal(f.listeners.size, 0)
  assert.equal(f.client.reportError(), false)
})

test("contains invalid payloads and render failures without exposing errors", async () => {
  const f = fixture(() => { throw new Error("PRIVATE_STACK") })
  f.init()
  f.snapshot(null)
  f.snapshot({ nodes: "not an array" })
  f.snapshot({ nodes: [{ id: "node" }] })
  assert.equal(f.sent.at(-1).value.beat.payload.code, "render_failed")
  assert.equal(f.client.reportError("runtime_error"), true)
  assert.equal(f.client.reportError("arbitrary-stack"), false)
  assert.equal(JSON.stringify(f.sent).includes("PRIVATE_STACK"), false)
  const asyncFailure = fixture(async () => { throw new Error("PRIVATE_REJECTION") })
  asyncFailure.init()
  asyncFailure.snapshot({ nodes: [] })
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(asyncFailure.sent.at(-1).value.beat.payload.code, "render_failed")
  assert.equal(JSON.stringify(asyncFailure.sent).includes("PRIVATE_REJECTION"), false)
  f.client.destroy()
  asyncFailure.client.destroy()
})

test("handles a disconnected parent and rejects missing renderers", () => {
  const f = fixture()
  f.scope.parent.postMessage = () => { throw new Error("closed") }
  f.init()
  assert.equal(f.client.reportError(), false)
  f.client.destroy()
  assert.throws(() => connectBeatTheme(null, f.scope), TypeError)
})

test("passes history gaps and the successful aggregation metadata unchanged", () => {
  const snapshots = []
  const f = fixture((value) => snapshots.push(value))
  f.init()
  const history = { from: 10, to: 20, series: { cpu: [{ timestamp: 12, value: 0 }, { timestamp: 13, value: null }] }, series_info: { cpu: { aggregation: "max", unit: "percent" } } }
  f.snapshot({ nodes: [{ id: "node" }], history })
  assert.deepEqual(snapshots[0].history, history)
  f.client.destroy()
})
