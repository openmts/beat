import { describe, expect, it } from "vitest"
import { cellDisplayModeError, maximumTableRows, panelCellDisplayMode, panelTableOrientation, sortWorkspaceFrameRows, sortWorkspaceTableRows, sortWorkspaceWideRows, tableCellStyle, tableOrientationError, tablePatch, workspaceFrameCSV, workspaceTableCSV, workspaceTableColumns, workspaceTableRows, workspaceWideCSV, workspaceWideColumns, workspaceWideRows } from "@/lib/workspace-table"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

const result = (): MetricQueryResult => ({
  from: "2026-10-17T00:00:00.000Z", to: "2026-10-17T01:00:00.000Z", interval_ms: 60000,
  series: [
    { ref_id: "B", node_id: "n2", metric: "memory", aggregation: "avg", label: "Memory", unit: "bytes", available_from: null, state: "ok",
      points: [{ timestamp: 3000, value: 30 }, { timestamp: 1000, value: null }] },
    { ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", label: "CPU", unit: "percent", available_from: null, state: "ok",
      points: [{ timestamp: 2000, value: 50 }, { timestamp: 4000, value: 10 }] },
  ],
})

const tablePanel = (patch: Partial<DashboardPanel> = {}): DashboardPanel => ({
  id: "panel-a", title: "Table", type: "table", size: "medium", legend: false, thresholds: [],
  series: [{ ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", color: "blue", label: "CPU" }], ...patch,
})

describe("workspace table field overrides", () => {
  it("formats rows with the unit declared by a matching override", () => {
    const rows = workspaceTableRows(result(), undefined, { panel: { overrides: [
      { matcher: { type: "by_name", value: "CPU" }, unit: "percent_per_second" },
    ] } })
    expect(rows.map((row) => [row.label, row.unit])).toEqual([["Memory", "bytes"], ["Memory", "bytes"], ["CPU", "percent_per_second"], ["CPU", "percent_per_second"]])
    expect(workspaceTableRows(result(), undefined, { panel: { overrides: [] } }).map((row) => row.unit))
      .toEqual(["bytes", "bytes", "percent", "percent"])
  })
})

describe("workspace table cell display", () => {
  it("defaults to text and rejects modes outside table panels", () => {
    expect(panelCellDisplayMode(tablePanel())).toBe("text")
    expect(panelCellDisplayMode(tablePanel({ cell_display_mode: "gradient" }))).toBe("gradient")
    expect(cellDisplayModeError(tablePanel({ cell_display_mode: "gradient" }))).toBeNull()
    expect(cellDisplayModeError(tablePanel({ cell_display_mode: "sparkle" as never }))).toBe("workspace.cell_display_invalid")
    expect(cellDisplayModeError(tablePanel({ type: "timeseries", cell_display_mode: "gradient" }))).toBe("workspace.cell_display_table_only")
  })

  it("keeps mapped colors in text mode and returns nothing without a mapping", () => {
    expect(tableCellStyle(tablePanel(), { value: 42 }, "#ff0000")).toEqual({ color: "#ff0000" })
    expect(tableCellStyle(tablePanel(), { value: 42 })).toBeUndefined()
    expect(tableCellStyle(tablePanel({ cell_display_mode: "color_text" }), { value: null }, "#ff0000")).toEqual({ color: "#ff0000" })
  })

  it("colors a cell from the per series threshold band instead of the panel band", () => {
    const panel = tablePanel({ cell_display_mode: "color_text", thresholds: [{ value: 0, color: "green" }], overrides: [
      { matcher: { type: "by_ref_id", value: "A" }, thresholds: [{ value: 0, color: "blue" }, { value: 90, color: "rose" }] },
    ] })
    const cell = (value: number, refID = "A") => ({ value, series: { ref_id: refID, label: "CPU" } })
    expect(tableCellStyle(panel, cell(90))).toEqual({ color: "#e11d48" })
    expect(tableCellStyle(panel, cell(50))).toEqual({ color: "#3b82f6" })
    // 未命中覆盖的序列仍然按面板阈值取色。
    expect(tableCellStyle(panel, cell(50, "B"))).toEqual({ color: "#16a34a" })
    expect(tableCellStyle(panel, { value: 50 })).toEqual({ color: "#16a34a" })
  })

  it("colors text, background and gradient from the threshold palette", () => {
    const panel = tablePanel({ cell_display_mode: "color_text", thresholds: [{ value: 0, color: "green" }, { value: 80, color: "rose" }] })
    expect(tableCellStyle(panel, { value: 90 })).toEqual({ color: "#e11d48" })
    expect(tableCellStyle({ ...panel, cell_display_mode: "color_background" }, { value: 90 })).toEqual({ backgroundColor: "#e11d48", color: "#f8fafc" })
    expect(tableCellStyle({ ...panel, cell_display_mode: "gradient" }, { value: 90 })).toEqual({ backgroundColor: "rgba(225, 29, 72, 0.85)" })
    expect(tableCellStyle({ ...panel, cell_display_mode: "gradient" }, { value: 40 })).toEqual({ backgroundColor: "rgba(22, 163, 74, 0.50)" })
  })
})

describe("workspace table rows", () => {
  /** 两条序列共享 2000 号桶：B 为空、A 有值，用于验证「只丢全空时间点」。 */
  const withGap = (): MetricQueryResult => ({
    from: "2026-10-17T00:00:00.000Z", to: "2026-10-17T01:00:00.000Z", interval_ms: 60000,
    series: [
      { ref_id: "B", node_id: "n2", metric: "memory", aggregation: "avg", label: "Memory", unit: "bytes", available_from: null, state: "ok",
        points: [{ timestamp: 3000, value: 30 }, { timestamp: 2000, value: null }] },
      { ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", label: "CPU", unit: "percent", available_from: null, state: "ok",
        points: [{ timestamp: 2000, value: 50 }, { timestamp: 4000, value: 10 }] },
    ],
  })

  it("flattens every sample with its series label and unit", () => {
    const rows = workspaceTableRows(withGap())
    expect(rows.map((row) => [row.timestamp, row.label, row.value])).toEqual([
      [3000, "Memory", 30], [2000, "Memory", null], [2000, "CPU", 50], [4000, "CPU", 10],
    ])
    expect(rows[0]!.unit).toBe("bytes")
    expect(rows[0]!.key).toBe("B-n2@3000")
  })

  it("keeps empty buckets by default and drops them for panels", () => {
    const padded = result()
    padded.series[0]!.points.unshift({ timestamp: 0, value: null })
    expect(workspaceTableRows(padded).map((row) => row.timestamp)).toEqual([0, 3000, 1000, 2000, 4000])
    expect(workspaceTableRows(padded, undefined, { dropEmptyBuckets: true }).map((row) => row.timestamp)).toEqual([3000, 2000, 4000])
    // 2000 号桶 CPU 有值，因此 Memory 的空值在面板视图里仍然保留。
    const rows = workspaceTableRows(withGap(), undefined, { dropEmptyBuckets: true })
    expect(rows.map((row) => [row.timestamp, row.refID, row.value])).toEqual([
      [3000, "B", 30], [2000, "B", null], [2000, "A", 50], [4000, "A", 10],
    ])
  })

  it("sorts by timestamp、series and value with nulls last", () => {
    const rows = workspaceTableRows(withGap())
    expect(sortWorkspaceTableRows(rows, "timestamp", "asc").map((row) => row.timestamp)).toEqual([2000, 2000, 3000, 4000])
    expect(sortWorkspaceTableRows(rows, "timestamp", "desc").map((row) => row.timestamp)).toEqual([4000, 3000, 2000, 2000])
    expect(sortWorkspaceTableRows(rows, "label", "asc").map((row) => row.label)).toEqual(["CPU", "CPU", "Memory", "Memory"])
    expect(sortWorkspaceTableRows(rows, "value", "asc").map((row) => row.value)).toEqual([10, 30, 50, null])
    expect(sortWorkspaceTableRows(rows, "value", "desc").map((row) => row.value)).toEqual([50, 30, 10, null])
  })

  it("exposes the display columns and the row cap", () => {
    expect(workspaceTableColumns).toEqual(["timestamp", "label", "value"])
    expect(maximumTableRows).toBe(100)
  })

  it("exports the given rows as CSV with ISO timestamps and empty cells for gaps", () => {
    const rows = workspaceTableRows(withGap())
    expect(workspaceTableCSV(rows.slice(0, 2), ["Timestamp", "Series", "Value"])).toBe([
      '"Timestamp","Series","Value"',
      '"1970-01-01T00:00:03.000Z","Memory","30"',
      '"1970-01-01T00:00:02.000Z","Memory",""',
    ].join("\r\n"))
  })
})

describe("workspace table orientation", () => {
  it("defaults to the historical long layout and rejects values outside table panels", () => {
    expect(panelTableOrientation(tablePanel())).toBe("long")
    expect(panelTableOrientation(tablePanel({ table_orientation: "wide" }))).toBe("wide")
    expect(tableOrientationError(tablePanel({ table_orientation: "wide" }))).toBeNull()
    expect(tableOrientationError(tablePanel({ table_orientation: "diagonal" as never }))).toBe("workspace.table_orientation_invalid")
    expect(tableOrientationError(tablePanel({ type: "timeseries", table_orientation: "wide" }))).toBe("workspace.table_orientation_table_only")
  })

  it("clears the layout when a table panel becomes another type and keeps it on table panels", () => {
    expect(tablePatch("table", tablePanel({ table_orientation: "wide" }))).toEqual({})
    expect(tablePatch("timeseries", tablePanel({ table_orientation: "wide" }))).toEqual({ table_orientation: undefined })
    expect(tablePatch("timeseries", tablePanel())).toEqual({})
  })

  it("builds one column per series and one row per timestamp", () => {
    const columns = workspaceWideColumns(result())
    expect(columns.map((column) => [column.refID, column.label, column.unit, column.key]))
      .toEqual([["B", "Memory", "bytes", "B-n2"], ["A", "CPU", "percent", "A-n1"]])
    // 空桶不占行：两条序列都缺的时间点不会出现，缺失的一侧保留 null。
    expect(workspaceWideRows(result(), columns)).toEqual([
      { key: "2000", timestamp: 2000, cells: [null, 50] },
      { key: "3000", timestamp: 3000, cells: [30, null] },
      { key: "4000", timestamp: 4000, cells: [null, 10] },
    ])
  })

  it("sorts by the time axis or a series column with gaps last", () => {
    const columns = workspaceWideColumns(result())
    const rows = workspaceWideRows(result(), columns)
    expect(sortWorkspaceWideRows(rows, null, "desc").map((row) => row.timestamp)).toEqual([4000, 3000, 2000])
    expect(sortWorkspaceWideRows(rows, 1, "asc").map((row) => row.cells[1])).toEqual([10, 50, null])
    expect(sortWorkspaceWideRows(rows, 1, "desc").map((row) => row.cells[1])).toEqual([50, 10, null])
    // 数值相同或缺失时回落到时间轴，保证结果稳定。
    expect(sortWorkspaceWideRows(rows, 0, "asc").map((row) => row.timestamp)).toEqual([3000, 2000, 4000])
  })

  it("exports the wide rows as CSV with ISO timestamps and empty gap cells", () => {
    const columns = workspaceWideColumns(result())
    expect(workspaceWideCSV(workspaceWideRows(result(), columns).slice(0, 2), ["Timestamp", "Memory", "CPU"])).toBe([
      '"Timestamp","Memory","CPU"',
      '"1970-01-01T00:00:02.000Z","","50"',
      '"1970-01-01T00:00:03.000Z","30",""',
    ].join("\r\n"))
  })
})

describe("workspace result frame table helpers", () => {
  const columns = [
    { key: "field", labelKey: "workspace.frame_field", value: false },
    { key: "calculation_min", labelKey: "workspace.calculation_min", value: true },
    { key: "calculation_max", labelKey: "workspace.calculation_max", value: true },
  ]
  const rows = [
    { key: "field_0", cells: [{ text: "Memory" }, { value: 5 }, { value: 30 }] },
    { key: "field_1", cells: [{ text: "CPU" }, { value: 10 }, { value: null }] },
  ]

  it("sorts key columns as text and value columns numerically with gaps last", () => {
    expect(sortWorkspaceFrameRows(rows, columns, 0, "asc").map((row) => row.cells[0].text)).toEqual(["CPU", "Memory"])
    expect(sortWorkspaceFrameRows(rows, columns, 0, "desc").map((row) => row.cells[0].text)).toEqual(["Memory", "CPU"])
    expect(sortWorkspaceFrameRows(rows, columns, 1, "asc").map((row) => row.cells[1].value)).toEqual([5, 10])
    // 缺样本的格子无论升降序都排在最后。
    expect(sortWorkspaceFrameRows(rows, columns, 2, "asc").map((row) => row.cells[2].value)).toEqual([30, null])
  })

  it("exports frames as CSV with the given headers and empty gap cells", () => {
    expect(workspaceFrameCSV(rows, ["Field", "Minimum", "Maximum"])).toBe([
      '"Field","Minimum","Maximum"',
      '"Memory","5","30"',
      '"CPU","10",""',
    ].join("\r\n"))
  })
})
