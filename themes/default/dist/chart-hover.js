import { append, element } from "./dom.js"
import { decimal, formatBytes, formatDate, formatRate, percentValue } from "./format.js"
import { state } from "./state.js"

export function chartTooltip() {
  const tooltip = element("div", "chart-tooltip")
  tooltip.hidden = true
  tooltip.setAttribute("role", "status")
  tooltip.setAttribute("aria-live", "polite")
  return tooltip
}

export function bindChartHover(task, drawChart) {
  task.canvas.addEventListener("pointermove", (event) => {
    if (task.hoverPinned && event.pointerType !== "mouse") return
    updateHover(task, drawChart, event)
  })
  task.canvas.addEventListener("pointerdown", (event) => {
    if (event.pointerType === "mouse") return
    if (!updateHover(task, drawChart, event)) return
    task.hoverPinned = true
    bindOutsideDismiss(task, drawChart)
  })
  task.canvas.addEventListener("pointerleave", () => {
    if (!task.hoverPinned) clearHover(task, drawChart)
  })
}

export function disposeChartHover(task) {
  task.hoverPinned = false
  unbindOutsideDismiss(task)
  task.tooltip.hidden = true
}

export function drawChartHover(context, palette, layout, hover) {
  context.save()
  context.strokeStyle = palette.muted
  context.globalAlpha = 0.65
  context.lineWidth = 1
  context.setLineDash([3, 3])
  context.beginPath()
  context.moveTo(hover.x, layout.insert.top)
  context.lineTo(hover.x, layout.insert.top + layout.plotHeight)
  context.stroke()
  context.setLineDash([])
  for (const row of hover.rows) {
    if (!row.point) continue
    const x = layout.insert.left + (row.point.timestamp - layout.bounds.from) / (layout.bounds.to - layout.bounds.from) * layout.plotWidth
    const y = layout.insert.top + (1 - row.point.value / layout.yMax) * layout.plotHeight
    context.globalAlpha = 1
    context.fillStyle = row.color
    context.beginPath()
    context.arc(x, y, 3, 0, Math.PI * 2)
    context.fill()
  }
  context.restore()
}

function clearHover(task, drawChart) {
  task.hoverPinned = false
  unbindOutsideDismiss(task)
  task.hover = undefined
  task.tooltip.hidden = true
  drawChart(task)
}

function updateHover(task, drawChart, event) {
  const layout = task.layout ?? drawChart(task)
  if (!layout) return false
  const rect = task.canvas.getBoundingClientRect()
  const x = event.clientX - rect.left
  if (x < layout.insert.left || x > layout.insert.left + layout.plotWidth) {
    clearHover(task, drawChart)
    return false
  }
  const ratio = (x - layout.insert.left) / layout.plotWidth
  const timestamp = layout.bounds.from + ratio * (layout.bounds.to - layout.bounds.from)
  const rows = layout.series.map((entry) => ({ ...entry, point: nearestPoint(entry.points, timestamp) }))
  task.hover = { x, timestamp, rows }
  drawChart(task)
  showTooltip(task, x, event.clientY - rect.top, timestamp, rows)
  return true
}

function bindOutsideDismiss(task, drawChart) {
  unbindOutsideDismiss(task)
  task.dismissPointerDown = (event) => {
    if (task.canvas.contains(event.target)) return
    clearHover(task, drawChart)
  }
  document.addEventListener("pointerdown", task.dismissPointerDown, true)
}

function unbindOutsideDismiss(task) {
  if (!task.dismissPointerDown) return
  document.removeEventListener("pointerdown", task.dismissPointerDown, true)
  task.dismissPointerDown = undefined
}

function nearestPoint(points, timestamp) {
  let nearest = null
  let distance = Number.POSITIVE_INFINITY
  for (const point of points) {
    if (!Number.isFinite(point.timestamp) || !Number.isFinite(point.value)) continue
    const current = Math.abs(point.timestamp - timestamp)
    if (current < distance) {
      nearest = point
      distance = current
    }
  }
  return nearest
}

function showTooltip(task, x, y, timestamp, rows) {
  const tooltip = task.tooltip
  tooltip.replaceChildren(element("span", "chart-tooltip-time", hoverTime(timestamp)))
  for (const row of rows) {
    const item = element("span", "chart-tooltip-row")
    const swatch = element("span", "chart-tooltip-swatch")
    swatch.style.background = row.color
    append(item, swatch, element("span", "chart-tooltip-label", row.label),
      element("span", "chart-tooltip-value", hoverValue(row.point?.value, task.spec.unit)))
    tooltip.append(item)
  }
  tooltip.hidden = false
  const hostWidth = task.canvas.clientWidth || task.layout?.width || 0
  const hostHeight = task.canvas.clientHeight || task.layout?.height || 0
  const tooltipWidth = tooltip.offsetWidth || 156
  const tooltipHeight = tooltip.offsetHeight || 34 + rows.length * 18
  const left = x + tooltipWidth + 20 > hostWidth ? x - tooltipWidth - 12 : x + 12
  const top = y - tooltipHeight / 2
  tooltip.style.left = `${Math.max(8, Math.min(left, hostWidth - tooltipWidth - 8))}px`
  tooltip.style.top = `${Math.max(8, Math.min(top, hostHeight - tooltipHeight - 8))}px`
}

function hoverTime(timestamp) {
  return formatDate(timestamp * 1000, {
    month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit",
  }, state.locale)
}

function hoverValue(value, unit) {
  if (!Number.isFinite(value)) return "—"
  if (unit === "percent") return percentValue(value)
  if (unit === "bytes/s") return formatRate(value)
  if (unit === "bytes") return formatBytes(value)
  if (unit === "ms") return `${decimal(value, 1)} ms`
  return `${decimal(value, 2)}${unit ? ` ${unit}` : ""}`
}
