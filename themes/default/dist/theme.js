// Default sandbox theme: a quiet fleet monitor built only from public snapshot data.
import { connectBeatTheme } from "./beat-sdk.js"
import { disposeCharts, resetCharts } from "./charts.js"
import { dashboard } from "./dashboard.js"
import { renderDetail } from "./detail.js"
import { runtime } from "./runtime.js"
import { state } from "./state.js"

let client
runtime.navigate = (path) => client.navigate(path)
runtime.setRange = (range) => client.setRange(range)

client = connectBeatTheme((snapshot) => {
  resetCharts()
  if (state.pendingRange !== null && snapshot.range?.mode === "relative" && snapshot.range.seconds === state.pendingRange) state.pendingRange = null
  state.snapshot = snapshot
  state.locale = typeof snapshot.locale === "string" && snapshot.locale ? snapshot.locale : state.locale
  document.documentElement.lang = state.locale
  document.documentElement.dataset.theme = snapshot.theme === "dark" ? "dark" : "light"
  if (snapshot.route?.kind === "node") {
    renderDetail(snapshot, state.locale)
    return
  }
  state.tab = "resources"
  dashboard(snapshot, state.locale)
})

addEventListener("pagehide", () => {
  disposeCharts()
  client.destroy()
})
