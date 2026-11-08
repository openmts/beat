import { connectBeatTheme } from "./beat-sdk.js"
import { disposeCharts, resetCharts } from "./charts.js"
import { renderPaper } from "./app.js"
import { runtime } from "./runtime.js"
import { state } from "./state.js"

let client
runtime.navigate = (path) => client.navigate(path)
runtime.setRange = (range) => client.setRange(range)

client = connectBeatTheme((snapshot) => {
  resetCharts()
  if (state.pendingRange !== null && snapshot.range?.mode === "relative" && snapshot.range.seconds === state.pendingRange) {
    state.pendingRange = null
  }
  state.snapshot = snapshot
  state.locale = typeof snapshot.locale === "string" && snapshot.locale ? snapshot.locale : state.locale
  if (snapshot.route?.kind !== "node") state.autoNavigatedTo = ""
  document.documentElement.lang = state.locale
  renderPaper(snapshot, state.locale)
})

addEventListener("pagehide", () => {
  disposeCharts()
  client.destroy()
})
