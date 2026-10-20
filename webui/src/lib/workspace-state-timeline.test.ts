import { describe, expect, it } from "vitest"
import { maxTimelineSegments, panelStateTimeline, stateTimeline, stateTimelineError, stateTimelinePatch, stateTimelineState, timelineShowsLabel } from "@/lib/workspace-state-timeline"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "statetimeline", ...overrides }
}

/** 构造一条从 from 到最后一个点之后 1 秒的序列，便于按比例核对色块宽度。 */
function resultOf(values: (number | null)[], overrides: Partial<MetricQueryResult> = {}): MetricQueryResult {
  const result = workspaceResult()
  const start = Date.parse(result.from)
  const points = values.map((value, index) => ({ timestamp: start + index * 1000, value }))
  return { ...result, to: new Date(start + values.length * 1000).toISOString(),
    series: [{ ...result.series[0], unit: "percent", points }], ...overrides }
}

const label = (series: { ref_id: string }) => `series-${series.ref_id}`

describe("state timeline options", () => {
  it("defaults to merging equal neighbours and automatic value labels", () => {
    expect(panelStateTimeline({ type: "statetimeline" })).toEqual({ merge: true, showValues: "auto" })
    expect(panelStateTimeline({ type: "statetimeline", timeline_merge: false, timeline_show_values: "always" })).toEqual({ merge: false, showValues: "always" })
    expect(panelStateTimeline({ type: "statetimeline", timeline_merge: true, timeline_show_values: "sometimes" })).toEqual({ merge: true, showValues: "auto" })
  })

  it("clears both fields when the panel stops being a state timeline", () => {
    expect(stateTimelinePatch("statetimeline")).toEqual({})
    expect(stateTimelinePatch("heatmap")).toEqual({ timeline_merge: undefined, timeline_show_values: undefined })
    expect(stateTimelinePatch("timeseries")).toEqual({ timeline_merge: undefined, timeline_show_values: undefined })
  })

  it("reports the same rejections as the server contract", () => {
    expect(stateTimelineError({ type: "statetimeline" })).toBeNull()
    expect(stateTimelineError({ type: "statetimeline", timeline_merge: false, timeline_show_values: "never" })).toBeNull()
    expect(stateTimelineError({ type: "statetimeline", timeline_show_values: "sometimes" })).toBe("workspace.state_timeline_show_values_invalid")
    expect(stateTimelineError({ type: "timeseries", timeline_merge: true })).toBe("workspace.state_timeline_statetimeline_only")
    expect(stateTimelineError({ type: "heatmap", timeline_show_values: "always" })).toBe("workspace.state_timeline_statetimeline_only")
    expect(stateTimelineError({ type: "statetimeline", timeline_show_values: "" })).toBeNull()
  })

  it("decides value labels from the block width", () => {
    expect(timelineShowsLabel("never", 1)).toBe(false)
    expect(timelineShowsLabel("auto", 0.15)).toBe(true)
    expect(timelineShowsLabel("auto", 0.14)).toBe(false)
    expect(timelineShowsLabel("always", 0.05)).toBe(true)
    expect(timelineShowsLabel("always", 0.01)).toBe(false)
  })
})

describe("state timeline states", () => {
  it("lets value mappings name and colour a state before thresholds apply", () => {
    const panel = panelOf({ value_mappings: [{ type: "range", from: 50, text: "Busy", color: "rose" }], thresholds: [{ value: 10, color: "amber" }] })
    const series = workspaceResult().series[0]
    expect(stateTimelineState(panel, series, 80)).toEqual({ key: "m:Busy:rose", text: "Busy", value: 80, color: "rose" })
  })

  it("colours unmapped samples from thresholds and falls back to the series colour", () => {
    const panel = panelOf({ thresholds: [{ value: 10, color: "amber" }, { value: 50, color: "rose" }], series: [{ ...workspaceDashboard().definition.panels[0].series[0], color: "cyan" }] })
    const series = workspaceResult().series[0]
    expect(stateTimelineState(panel, series, 5)).toMatchObject({ key: "v:5", text: null, color: "cyan" })
    expect(stateTimelineState(panel, series, 60)).toMatchObject({ color: "rose" })
    expect(stateTimelineState(panel, { ...series, ref_id: "B" }, 60)).toMatchObject({ color: "rose" })
  })

  it("colours a sample from the threshold band of the matched series", () => {
    const panel = panelOf({ thresholds: [{ value: 10, color: "amber" }], overrides: [
      { matcher: { type: "by_ref_id", value: "A" }, thresholds: [{ value: 50, color: "violet" }] },
    ] })
    const series = workspaceResult().series[0]
    // 命中覆盖的序列用自己的色带：10 落在覆盖区间之下，回到序列自身颜色；60 命中紫色。
    expect(stateTimelineState(panel, series, 20)).toMatchObject({ color: "blue" })
    expect(stateTimelineState(panel, series, 60)).toMatchObject({ color: "violet" })
    // 未命中的序列仍按面板阈值取色。
    expect(stateTimelineState(panel, { ...series, ref_id: "B" }, 60)).toMatchObject({ color: "amber" })
  })

  it("treats a missing sample as no state unless a mapping matches it", () => {
    const series = workspaceResult().series[0]
    expect(stateTimelineState(panelOf(), series, null)).toBeNull()
    expect(stateTimelineState(panelOf({ value_mappings: [{ type: "special", match: "null", text: "No data", color: "slate" }] }), series, null)).toEqual({ key: "m:No data:slate", text: "No data", value: null, color: "slate" })
  })
})

describe("state timeline rows", () => {
  it("merges equal neighbours into one block and splits when the state changes", () => {
    const timeline = stateTimeline(panelOf(), resultOf([1, 1, 1, 2, 2]), { label })
    expect(timeline.rows).toHaveLength(1)
    expect(timeline.rows[0].segments).toHaveLength(2)
    expect(timeline.rows[0].segments[0]).toMatchObject({ state: { key: "v:1" } })
    expect(timeline.rows[0].segments[0].to - timeline.rows[0].segments[0].from).toBe(3000)
    expect(timeline.rows[0].segments[1].to - timeline.rows[0].segments[1].from).toBe(2000)
    expect(timeline.segments).toBe(2)
  })

  it("keeps every sample as its own block when merging is switched off", () => {
    const timeline = stateTimeline(panelOf({ timeline_merge: false }), resultOf([1, 1, 1, 2, 2]), { label })
    expect(timeline.rows[0].segments).toHaveLength(5)
    expect(timeline.rows[0].segments.map((segment) => segment.state?.key)).toEqual(["v:1", "v:1", "v:1", "v:2", "v:2"])
  })

  it("keeps missing samples as gaps instead of inventing a state", () => {
    const timeline = stateTimeline(panelOf(), resultOf([1, null, 1]), { label })
    expect(timeline.rows[0].segments.map((segment) => segment.state)).toMatchObject([{ key: "v:1" }, null, { key: "v:1" }])
    expect(timeline.segments).toBe(3)
  })

  it("bounds every row to the block budget on long ranges", () => {
    const values = Array.from({ length: 600 }, (_, index) => index)
    const timeline = stateTimeline(panelOf(), resultOf(values), { label, budget: 120 })
    expect(timeline.rows[0].segments.length).toBeLessThanOrEqual(120)
    expect(timeline.rows[0].segments[0].from).toBe(Date.parse(resultOf(values).from))
    expect(timeline.rows[0].segments.at(-1)!.to).toBe(Date.parse(resultOf(values).to))
    expect(maxTimelineSegments).toBe(120)
  })

  it("lists one row per series and ranks states by block count", () => {
    const result = resultOf([1, 2, 1])
    result.series.push({ ...result.series[0], ref_id: "B", label: "Memory usage", points: result.series[0].points.map((point) => ({ ...point, value: 2 })) })
    const timeline = stateTimeline(panelOf(), result, { label })
    expect(timeline.rows.map((row) => row.label)).toEqual(["series-A", "series-B"])
    // v:1 在 A 行出现两块，排在只出现一块的 v:2 之前。
    expect(timeline.states.map((entry) => entry.state.key)).toEqual(["v:1", "v:2"])
    expect(timeline.states[0].count).toBe(2)
  })

  it("drops a row whose samples are all missing", () => {
    expect(stateTimeline(panelOf(), resultOf([null, null, null]), { label })).toMatchObject({ rows: [], segments: 0 })
  })

  it("returns no rows for a result without samples", () => {
    const empty: MetricQueryResult = { ...workspaceResult(), series: [{ ...workspaceResult().series[0], points: [] }] }
    expect(stateTimeline(panelOf(), empty, { label })).toMatchObject({ rows: [], states: [], segments: 0 })
  })
})
