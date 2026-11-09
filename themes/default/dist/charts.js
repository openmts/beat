import { append, element } from "./dom.js"
import { decimal, formatBytes, formatDate, formatRate, percentValue } from "./format.js"
import { strings } from "./i18n.js"
import { chartRuntime, chartTasks, state } from "./state.js"

function numericValues(snapshot, keys) {
  const values = []
  for (const key of keys) {
    for (const point of snapshot.history?.series?.[key] ?? []) if (Number.isFinite(point.value)) values.push(point.value)
  }
  return values
}

function lastValue(snapshot, key) {
  const points = snapshot.history?.series?.[key] ?? []
  for (let index = points.length - 1; index >= 0; index -= 1) if (Number.isFinite(points[index].value)) return points[index].value
  return NaN
}

function chartNote(snapshot, spec, locale) {
  const t = strings(locale)
  const values = numericValues(snapshot, spec.keys)
  if (!values.length) return t.noSample
  const peak = Math.max(...values)
  if (spec.keys.length > 1) {
    return spec.keys.map((key, index) => `${index ? "↑" : "↓"} ${spec.unit === "bytes/s" ? formatRate(lastValue(snapshot, key)) : decimal(lastValue(snapshot, key), 2)}`).join("  ")
  }
  if (spec.unit === "percent") return `${t.peak} ${percentValue(peak)}`
  if (spec.unit === "bytes/s") return `${t.peak} ${formatRate(peak)}`
  if (spec.unit === "bytes") return `${t.peak} ${formatBytes(peak)}`
  return `${t.peak} ${decimal(peak, 2)}${spec.unit ? ` ${spec.unit}` : ""}`
}

function legend(labels) {
  const root = element("div", "legend")
  labels.forEach((label, index) => {
    const item = element("span", "legend-item")
    const swatch = element("span", `legend-swatch${index ? " secondary" : ""}`)
    append(item, swatch, element("span", "", label))
    root.append(item)
  })
  return root
}

export function chartBlock(snapshot, spec, locale) {
  const block = element("section", "chart-block")
  const head = element("div", "chart-head")
  const title = element("h2", "chart-title", spec.title)
  const note = element("span", "chart-note", chartNote(snapshot, spec, locale))
  append(head, title, note)
  const wrap = element("div", "chart-wrap")
  const canvas = element("canvas", "chart-canvas")
  canvas.setAttribute("role", "img")
  canvas.setAttribute("aria-label", `${spec.title}: ${note.textContent}`)
  wrap.append(canvas)
  append(block, head, wrap)
  if (spec.labels?.length > 1) block.append(legend(spec.labels))
  chartTasks.push({ canvas, spec })
  return block
}

export function networkChartBlock(title, source, bounds, locale) {
  const t = strings(locale)
  const series = Array.isArray(source) && source.length && Array.isArray(source[0]?.points)
    ? source
    : [{ label: title, points: source }]
  const values = series.flatMap((entry) => entry.points.map((point) => point.latency_ms)).filter(Number.isFinite)
  const latest = values.length ? values[values.length - 1] : NaN
  const peak = values.length ? Math.max(...values) : NaN
  const note = values.length ? `${t.latest} ${decimal(latest, 1)} ms · ${t.peak} ${decimal(peak, 1)} ms` : t.noSample
  const root = element("div", "network-chart")
  root.dataset.series = String(series.length)
  root.dataset.carriers = series.map((entry) => entry.carrier || "custom").join(",")
  const wrap = element("div", "chart-wrap network-chart-wrap")
  const canvas = element("canvas", "chart-canvas network-canvas")
  canvas.setAttribute("role", "img")
  canvas.setAttribute("aria-label", `${title}: ${note}`)
  wrap.append(canvas)
  append(root, element("p", "network-chart-note", note), wrap)
  if (series.length > 1) {
    const labels = element("div", "legend")
    for (const entry of series) {
      const item = element("span", "legend-item")
      const swatch = element("span", "legend-swatch")
      swatch.style.background = networkCarrierColor(entry.carrier)
      append(item, swatch, element("span", "", entry.label || entry.carrier || title))
      labels.append(item)
    }
    root.append(labels)
  }
  chartTasks.push({
    canvas, spec: { title, unit: "ms" }, bounds,
    series: series.map((entry) => ({
      points: entry.points.map((point) => ({ timestamp: point.timestamp, value: point.latency_ms })),
      color: networkCarrierColor(entry.carrier),
    })),
  })
  return root
}

function networkCarrierColor(carrier) {
  if (carrier === "mobile") return "#2563eb"
  if (carrier === "unicom") return "#dc2626"
  if (carrier === "telecom") return "#16a34a"
  return ""
}

function chartPalette() {
  const styles = getComputedStyle(document.documentElement)
  return {
    text: styles.getPropertyValue("--text").trim() || "#242424",
    muted: styles.getPropertyValue("--muted").trim() || "#858585",
    line: styles.getPropertyValue("--line").trim() || "#e6e6e6",
    plot: styles.getPropertyValue("--plot").trim() || "#fafafa",
  }
}

function chartBounds(snapshot) {
  const history = snapshot.history
  const points = Object.values(history?.series ?? {}).flat()
  const timestamps = points.map((point) => point.timestamp).filter(Number.isFinite)
  const from = Number.isFinite(history?.from) ? history.from : Math.min(...timestamps)
  const to = Number.isFinite(history?.to) ? history.to : Math.max(...timestamps)
  return { from: Number.isFinite(from) ? from : 0, to: Number.isFinite(to) && to > from ? to : from + 1 }
}

function chartYMax(values, unit) {
  const maximum = values.length ? Math.max(...values) : 0
  if (!Number.isFinite(maximum) || maximum <= 0) return unit === "percent" ? 5 : 1
  const padded = maximum * 1.12
  if (unit === "percent") return Math.min(100, Math.max(4, Math.ceil(padded / 4) * 4))
  const magnitude = 10 ** Math.floor(Math.log10(padded))
  return Math.ceil(padded / magnitude) * magnitude
}

function axisValue(value, unit) {
  if (unit === "percent") return `${decimal(value, 0)}%`
  if (unit === "bytes/s") return formatRate(value)
  if (unit === "bytes") return formatBytes(value)
  if (unit === "ms") return `${decimal(value, value < 10 ? 1 : 0)} ms`
  return decimal(value, value < 10 ? 1 : 0)
}

function chartTime(timestamp, span, locale) {
  const options = span <= 172800
    ? { hour: "2-digit", minute: "2-digit" }
    : { month: "2-digit", day: "2-digit" }
  return formatDate(timestamp * 1000, options, locale)
}

function prepareCanvas(canvas) {
  const width = canvas.clientWidth
  const height = canvas.clientHeight
  if (!width || !height) return null
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  if (canvas.width !== Math.round(width * ratio) || canvas.height !== Math.round(height * ratio)) {
    canvas.width = Math.round(width * ratio)
    canvas.height = Math.round(height * ratio)
  }
  const context = canvas.getContext("2d")
  if (!context) return null
  context.setTransform(ratio, 0, 0, ratio, 0, 0)
  return { context, width, height }
}

function drawYAxis(context, palette, insert, plotWidth, plotHeight, yMax, unit) {
  context.font = "10px ui-sans-serif, system-ui, sans-serif"
  context.textBaseline = "middle"
  context.lineWidth = 1
  context.setLineDash([2, 4])
  for (let index = 0; index <= 4; index += 1) {
    const y = insert.top + plotHeight * index / 4
    context.strokeStyle = palette.line
    context.beginPath()
    context.moveTo(insert.left, y)
    context.lineTo(insert.left + plotWidth, y)
    context.stroke()
    context.fillStyle = palette.muted
    context.textAlign = "right"
    context.fillText(axisValue(yMax * (4 - index) / 4, unit), insert.left - 9, y)
  }
}

function drawXAxis(context, palette, insert, plotWidth, height, bounds, locale) {
  for (let index = 0; index <= 4; index += 1) {
    const x = insert.left + plotWidth * index / 4
    if (index) {
      context.strokeStyle = palette.line
      context.beginPath()
      context.moveTo(x, insert.top)
      context.lineTo(x, insert.top + insert.plotHeight)
      context.stroke()
    }
    context.fillStyle = palette.muted
    context.textAlign = index === 0 ? "left" : index === 4 ? "right" : "center"
    context.fillText(chartTime(bounds.from + (bounds.to - bounds.from) * index / 4, bounds.to - bounds.from, locale), x, height - 11)
  }
}

function drawSeries(context, options) {
  let started = false
  context.strokeStyle = options.color
  context.lineWidth = 1.6
  context.lineJoin = "round"
  context.lineCap = "round"
  context.beginPath()
  for (const point of options.points) {
    if (!Number.isFinite(point.timestamp) || !Number.isFinite(point.value)) {
      started = false
      continue
    }
    const x = options.insert.left + Math.max(0, Math.min(1, (point.timestamp - options.bounds.from) / (options.bounds.to - options.bounds.from))) * options.plotWidth
    const y = options.insert.top + (1 - Math.max(0, Math.min(1, point.value / options.yMax))) * options.plotHeight
    if (!started) context.moveTo(x, y)
    else context.lineTo(x, y)
    started = true
  }
  context.stroke()
}

function drawChart(task) {
  const prepared = prepareCanvas(task.canvas)
  if (!prepared) return
  const { context, width, height } = prepared
  const palette = chartPalette()
  context.clearRect(0, 0, width, height)
  context.fillStyle = palette.plot
  context.fillRect(0, 0, width, height)
  const insert = { left: 56, right: 10, top: 13, bottom: 25 }
  const plotWidth = Math.max(1, width - insert.left - insert.right)
  const plotHeight = Math.max(1, height - insert.top - insert.bottom)
  insert.plotHeight = plotHeight
  const bounds = task.bounds ?? chartBounds(state.snapshot)
  const series = task.series ?? task.spec.keys.map((key, index) => ({ points: state.snapshot.history?.series?.[key] ?? [], index }))
  const values = series.flatMap((entry) => entry.points.map((point) => point.value)).filter(Number.isFinite)
  const yMax = chartYMax(values, task.spec.unit)
  drawYAxis(context, palette, insert, plotWidth, plotHeight, yMax, task.spec.unit)
  drawXAxis(context, palette, insert, plotWidth, height, bounds, state.locale)
  context.setLineDash([])
  if (!values.length) {
    context.fillStyle = palette.muted
    context.textAlign = "center"
    context.font = "12px ui-sans-serif, system-ui, sans-serif"
    context.fillText(strings(state.locale).noSample, insert.left + plotWidth / 2, insert.top + plotHeight / 2)
    return
  }
  series.forEach((entry, index) => drawSeries(context, {
    points: entry.points, bounds, yMax, insert, plotWidth, plotHeight,
    color: entry.color || (index ? palette.muted : palette.text),
  }))
}

function drawCharts() {
  for (const task of chartTasks) drawChart(task)
}

export function registerCharts() {
  if (!chartTasks.length) return
  if (chartRuntime.frame) cancelAnimationFrame(chartRuntime.frame)
  chartRuntime.frame = requestAnimationFrame(() => {
    chartRuntime.frame = 0
    if (typeof ResizeObserver === "function") {
      chartRuntime.observer ??= new ResizeObserver(() => drawCharts())
      chartRuntime.observer.disconnect()
      for (const task of chartTasks) chartRuntime.observer.observe(task.canvas)
    }
    drawCharts()
  })
}

export function resetCharts() {
  chartTasks.length = 0
  if (chartRuntime.frame) cancelAnimationFrame(chartRuntime.frame)
  chartRuntime.frame = 0
  chartRuntime.observer?.disconnect()
}

export function disposeCharts() {
  resetCharts()
}
