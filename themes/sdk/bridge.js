// Beat public theme SDK, bridge protocol 2. MIT; see ../LICENSE.
const errorCodes = new Set(["render_failed", "unsupported_data", "runtime_error"])

/** @typedef {{timestamp: number, value: number|null}} ThemeHistoryPoint Unix seconds; null is a missing bucket. */
/** @typedef {{aggregation: 'avg'|'min'|'max'|'sum'|'last'|'rate', unit: string}} ThemeHistorySeriesInfo */
/** @typedef {{telemetry_at?: string|null, telemetry_age_seconds?: number|null, telemetry_state?: 'fresh'|'delayed'|'stale'|'unavailable'}} ThemeTelemetry */
// 遥测新鲜度独立于节点在线状态；unavailable 时采集时间和年龄为 null。

export function connectBeatTheme(render, scope = window) {
  if (typeof render !== "function") throw new TypeError("A snapshot renderer is required")
  const origin = new URL(scope.location.href).origin
  const state = { instance: "", closed: false, visible: new Set() }
  const send = (type, payload) => {
    if (!state.instance || state.closed) return false
    try {
      scope.parent.postMessage({ beat: { version: 2, instance_id: state.instance, type, ...(payload ? { payload } : {}) } }, origin)
      return true
    } catch { return false }
  }
  const reportError = (code = "render_failed") => errorCodes.has(code) && send("error", { code })
  const receive = (event) => {
    if (state.closed || event.source !== scope.parent || event.origin !== origin) return
    const message = event.data?.beat
    if (!message || message.version !== 2) return
    if (message.type === "init" && /^[a-f0-9]{32}$/.test(message.instance_id)) {
      state.instance = message.instance_id
      state.visible.clear()
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
  state.visible = new Set(snapshot.nodes.filter((node) => typeof node?.id === "string").map((node) => `/node/${node.id}`))
  try {
    Promise.resolve(callbacks.render(snapshot)).catch(() => callbacks.reportError("render_failed"))
  } catch { callbacks.reportError("render_failed") }
}
