// Beat public theme SDK, bridge protocol 3. MIT; see ../LICENSE.
const errorCodes = new Set(["render_failed", "unsupported_data", "runtime_error"])
const relativeRangeSeconds = new Set([300, 900, 3600, 21600, 86400, 604800, 2592000])
const maximumRangeMilliseconds = 31 * 24 * 60 * 60 * 1000

/** @typedef {{timestamp: number, value: number|null}} ThemeHistoryPoint Unix seconds; null is a missing bucket. */
/** @typedef {{aggregation: 'avg'|'min'|'max'|'sum'|'last'|'rate', unit: string}} ThemeHistorySeriesInfo */
/** @typedef {{telemetry_at?: string|null, telemetry_age_seconds?: number|null, telemetry_state?: 'fresh'|'delayed'|'stale'|'unavailable'}} ThemeTelemetry */
// 遥测新鲜度独立于节点在线状态；unavailable 时采集时间和年龄为 null。

export function connectBeatTheme(render, scope = window) {
  if (typeof render !== "function") throw new TypeError("A snapshot renderer is required")
  const origin = new URL(scope.location.href).origin
  const state = { instance: "", closed: false, visible: new Set(), routeKind: "" }
  const send = (type, payload) => {
    if (!state.instance || state.closed) return false
    try {
      scope.parent.postMessage({ beat: { version: 3, instance_id: state.instance, type, ...(payload ? { payload } : {}) } }, origin)
      return true
    } catch { return false }
  }
  const reportError = (code = "render_failed") => errorCodes.has(code) && send("error", { code })
  const receive = (event) => {
    if (state.closed || event.source !== scope.parent || event.origin !== origin) return
    const message = event.data?.beat
    if (!message || message.version !== 3) return
    if (message.type === "init" && /^[a-f0-9]{32}$/.test(message.instance_id)) {
      state.instance = message.instance_id
      state.visible.clear()
      state.routeKind = ""
      send("ready")
    } else if (message.type === "snapshot" && message.instance_id === state.instance) {
      consumeSnapshot(message.payload, state, { render, reportError })
    }
  }
  scope.addEventListener("message", receive)
  return {
    navigate(path) {
      if (typeof path !== "string" || (path !== "/" && !state.visible.has(path))) return false
      return send("navigate", { path })
    },
    setRange(range) {
      const payload = normalizeRange(range)
      if (!payload || state.routeKind !== "node") return false
      return send("setRange", payload)
    },
    reportError,
    destroy() {
      state.closed = true
      state.instance = ""
      state.visible.clear()
      scope.removeEventListener("message", receive)
    },
  }
}

function consumeSnapshot(snapshot, state, callbacks) {
  if (!snapshot || typeof snapshot !== "object" || !Array.isArray(snapshot.nodes)) {
    callbacks.reportError("unsupported_data")
    return
  }
  state.routeKind = snapshot.route?.kind === "node" ? "node" : "dashboard"
  state.visible = new Set(snapshot.nodes.filter((node) => typeof node?.id === "string").map((node) => `/node/${node.id}`))
  try {
    Promise.resolve(callbacks.render(snapshot)).catch(() => callbacks.reportError("render_failed"))
  } catch { callbacks.reportError("render_failed") }
}

function normalizeRange(range) {
  if (!plainObject(range)) return null
  if (range.mode === "relative") {
    if (!exactKeys(range, ["mode", "seconds"]) || !relativeRangeSeconds.has(range.seconds)) return null
    return { mode: "relative", seconds: range.seconds }
  }
  if (range.mode !== "absolute" || !exactKeys(range, ["mode", "from", "to"])) return null
  const from = timestamp(range.from)
  const to = timestamp(range.to)
  if (from === null || to === null || from >= to || to - from > maximumRangeMilliseconds) return null
  return { mode: "absolute", from: new Date(from).toISOString(), to: new Date(to).toISOString() }
}

function plainObject(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false
  const prototype = Object.getPrototypeOf(value)
  return prototype === Object.prototype || prototype === null
}

function exactKeys(value, keys) {
  return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
}

function timestamp(value) {
  if (typeof value !== "string" || value.length === 0 || value.length > 64) return null
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}
