import { describe, expect, it } from "vitest"
import { exportDashboardJSON, importDashboardJSON, workspaceShareLink } from "./workspace-portable"
import type { DashboardDefinition } from "@/types/metric-workspace"

function definition(overrides: Partial<DashboardDefinition> = {}): DashboardDefinition {
  return { version: 1, title: "Fleet health", description: "Steady view", time_range_seconds: 3600, refresh_seconds: 15,
    variables: [{ name: "node", type: "node", label: "Node" }],
    panels: [{ id: "cpu", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [{ value: 80, color: "amber" }],
      series: [{ ref_id: "a", node_id: "$node", metric: "cpu.usage", aggregation: "avg", color: "blue", label: "CPU" }] }], ...overrides }
}

function imported(payload: unknown): DashboardDefinition {
  return importDashboardJSON(JSON.stringify(payload))
}

describe("dashboard variable options", () => {
  it("keeps multi-select and option filters through export and import", () => {
    const source = definition({ variables: [{ name: "node", type: "node", label: "Node", multi: true, include_all: true, regex: "^hk-[0-9]+$" }] })
    expect(importDashboardJSON(exportDashboardJSON(source)).variables).toEqual(source.variables)
  })

  it("drops a non-boolean multi flag but refuses an unusable option filter", () => {
    const variables = [{ name: "node", type: "node", label: "Node", multi: "yes" }]
    expect(importDashboardJSON(exportDashboardJSON(definition({ variables: variables as never }))).variables)
      .toEqual([{ name: "node", type: "node", label: "Node" }])
    expect(() => importDashboardJSON(exportDashboardJSON(definition({ variables: [{ name: "node", type: "node", label: "Node", include_all: true }] }))))
      .toThrow("workspace.import_invalid")
    for (const regex of ["(", "a".repeat(65)]) {
      const payload = definition({ variables: [{ name: "node", type: "node", label: "Node", regex }] })
      expect(() => importDashboardJSON(exportDashboardJSON(payload))).toThrow("workspace.import_invalid")
    }
  })
})

describe("panel value mappings", () => {
  it("round-trips mapped text and colour", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "stat",
      value_mappings: [{ type: "value", value: 0, text: "offline", color: "rose" }, { type: "range", from: 1, text: "warm" }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("value_mappings")
  })

  it("round-trips the stat text size and refuses unknown values", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "stat", stat_text_mode: "value", stat_text_size: "xl" }
    const roundTripped = importDashboardJSON(exportDashboardJSON(source))
    expect(roundTripped.panels[0]).toMatchObject({ stat_text_mode: "value", stat_text_size: "xl" })
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("stat_text_size")
    const base = { ...definition().panels[0], type: "stat" }
    const cases: Record<string, unknown>[] = [
      { ...base, stat_text_size: "xxl" },
      { ...base, stat_text_size: 3 },
      { ...definition().panels[0], stat_text_size: "xl" },
    ]
    for (const panel of cases) expect(() => imported({ ...definition(), panels: [panel] })).toThrow("workspace.import_invalid")
  })

  it("round-trips the stat orientation and refuses unknown values", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "stat", stat_orientation: "horizontal" }
    const roundTripped = importDashboardJSON(exportDashboardJSON(source))
    expect(roundTripped.panels[0]).toMatchObject({ stat_orientation: "horizontal" })
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("stat_orientation")
    const base = { ...definition().panels[0], type: "stat" }
    const cases: Record<string, unknown>[] = [
      { ...base, stat_orientation: "diagonal" },
      { ...base, stat_orientation: 3 },
      { ...base, stat_orientation: "" },
      { ...definition().panels[0], stat_orientation: "vertical" },
    ]
    for (const panel of cases.filter((panel) => panel.stat_orientation !== "")) {
      expect(() => imported({ ...definition(), panels: [panel] })).toThrow("workspace.import_invalid")
    }
    // 空串与省略等价：导入既不报错也不落下字段。
    expect(imported({ ...definition(), panels: [{ ...base, stat_orientation: "" }] }).panels[0]).not.toHaveProperty("stat_orientation")
  })

  it("refuses malformed, unbounded or timeseries mappings", () => {
    const base = { ...definition().panels[0], type: "stat" }
    const cases = [
      [{ type: "regex", text: "x" }],
      [{ type: "value", value: 0 }],
      [{ type: "value", text: "x" }],
      [{ type: "range", from: 2, to: 2, text: "x" }],
      [{ type: "special", match: "nan", text: "x" }],
      [{ type: "value", value: 0, text: "x".repeat(33) }],
      Array.from({ length: 9 }, () => ({ type: "value", value: 1, text: "x" })),
    ]
    for (const value_mappings of cases) expect(() => imported({ ...definition(), panels: [{ ...base, value_mappings }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], value_mappings: [{ type: "value", value: 1, text: "x" }] }] }))
      .toThrow("workspace.import_invalid")
  })
})

describe("panel state timeline", () => {
  it("round-trips the merge flag, the value labels and its value mappings", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "statetimeline", timeline_merge: false, timeline_show_values: "always",
      value_mappings: [{ type: "value", value: 1, text: "up", color: "green" }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("timeline_merge")
  })

  it("refuses unknown modes, non-boolean merge flags and options on other panel types", () => {
    const base = { ...definition().panels[0], type: "statetimeline" }
    const cases = [{ timeline_show_values: "sometimes" }, { timeline_merge: "yes" }, { timeline_merge: null }]
    for (const patch of cases) expect(() => imported({ ...definition(), panels: [{ ...base, ...patch }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], timeline_merge: true }] })).toThrow("workspace.import_invalid")
    expect(imported({ ...definition(), panels: [{ ...base, timeline_show_values: "" }] }).panels[0]).not.toHaveProperty("timeline_show_values")
  })
})

describe("panel histogram", () => {
  it("round-trips the bucket count, the count scale, the combine flag and the colour", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "histogram", histogram_buckets: 24, histogram_scale: "log", histogram_color: "violet", histogram_merge: true }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("histogram_buckets")
  })

  it("refuses unknown enums, non-boolean merge flags and options on other panel types", () => {
    const base = { ...definition().panels[0], type: "histogram" }
    const cases = [{ histogram_scale: "sqrt" }, { histogram_color: "magenta" }, { histogram_buckets: 65 }, { histogram_merge: "yes" }]
    for (const patch of cases) expect(() => imported({ ...definition(), panels: [{ ...base, ...patch }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], histogram_merge: true }] })).toThrow("workspace.import_invalid")
    expect(imported({ ...definition(), panels: [{ ...base, histogram_scale: "" }] }).panels[0]).not.toHaveProperty("histogram_scale")
  })
})

describe("panel geo map", () => {
  it("round-trips the location table, the layer, the viewport and the tile template", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "geomap", geo_zoom: 3, geo_marker_size: 12, geo_layer: "heatmap",
      geo_tile_url: "https://tiles.example.com/{z}/{x}/{y}.png", geo_center_latitude: 31.23, geo_center_longitude: 121.47,
      geo_locations: [{ match: "node-one", label: "机房 A", latitude: 39.9042, longitude: 116.4074 }, { match: "node-two", latitude: -33.8688, longitude: 151.2093 }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("geo_locations")
  })

  it("refuses invalid locations, unknown layers and geo options on other panel types", () => {
    const base = { ...definition().panels[0], type: "geomap", geo_locations: [{ match: "node-one", latitude: 1, longitude: 2 }] }
    const cases = [
      { geo_locations: [] },
      { geo_locations: [{ match: "", latitude: 1, longitude: 2 }] },
      { geo_locations: [{ match: "node-one", latitude: 1, longitude: 2 }, { match: "NODE-ONE", latitude: 1, longitude: 2 }] },
      { geo_locations: [{ match: "node-one", latitude: 90, longitude: 2 }] },
      { geo_locations: [{ match: "node-one", latitude: 1, longitude: 181 }] },
      { geo_locations: [{ match: "node-one", latitude: "1", longitude: 2 }] },
      { geo_layer: "polygons" },
      { geo_zoom: 9 },
      { geo_marker_size: 1 },
      { geo_center_latitude: 10 },
      { geo_center_latitude: 10, geo_center_longitude: 181 },
      { geo_tile_url: "ftp://t/{z}/{x}/{y}.png" },
      { geo_tile_url: "https://t/{z}/{x}.png" },
    ]
    for (const patch of cases) expect(() => imported({ ...definition(), panels: [{ ...base, ...patch }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], geo_zoom: 2 }] })).toThrow("workspace.import_invalid")
    // 缺省值不落盘：空字符串模板与缺省图层在导入后仍然是缺省状态。
    expect(imported({ ...definition(), panels: [{ ...base, geo_tile_url: "", geo_layer: "" }] }).panels[0]).not.toHaveProperty("geo_tile_url")
  })
})

describe("panel table layout", () => {
  it("round-trips the wide layout, drops the default and refuses it outside table panels", () => {
    const wide = { ...definition().panels[0], type: "table" as const, table_orientation: "wide" as const }
    const roundTripped = importDashboardJSON(exportDashboardJSON({ ...definition(), panels: [wide] }))
    expect(roundTripped.panels[0]).toMatchObject({ table_orientation: "wide" })
    // long 是缺省形态：导入不留下冗余字段，缺省的表格面板也不会凭空多出它。
    expect(imported({ ...definition(), panels: [{ ...wide, table_orientation: "long" }] }).panels[0]).not.toHaveProperty("table_orientation")
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("table_orientation")
    const cases: Record<string, unknown>[] = [
      { ...wide, table_orientation: "diagonal" },
      { ...wide, table_orientation: 3 },
      { ...definition().panels[0], table_orientation: "wide" },
    ]
    for (const panel of cases) expect(() => imported({ ...definition(), panels: [panel] })).toThrow("workspace.import_invalid")
  })
})

describe("panel transformations", () => {
  it("round-trips every documented step", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [
      { kind: "rename_by_regex", pattern: "^(.*)$", replacement: "$1 total" },
      { kind: "filter_by_value", comparison: "gte", value: 0 },
      { kind: "sort_by", statistic: "mean", order: "asc" },
      { kind: "limit", count: 3 },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(importDashboardJSON(exportDashboardJSON(definition())).panels[0]).not.toHaveProperty("transformations")
    expect(imported({ ...definition(), panels: [{ ...definition().panels[0], transformations: [] }] }).panels[0])
      .not.toHaveProperty("transformations")
  })

  it("round-trips a binary operation step", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [
      { kind: "binary_operation", left: "used", operator: "/", right: "/^total/", alias: "usage" },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("round-trips a group by step with multiple calculations and an alias", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [
      { kind: "group_by", group_by: "node_id", calculations: ["sum", "count"], alias: "Cluster" },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("round-trips a series to rows reduce step", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [{ kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("round-trips a reduce step", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [{ kind: "reduce", calculation: "percent_change" }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("round-trips a group by step", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], transformations: [{ kind: "group_by", group_by: "node_id", calculation: "count" }] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("refuses unknown kinds, mixed fields and unbounded steps", () => {
    const cases = [
      [{ kind: "organize" }],
      [{ kind: "reduce" }],
      [{ kind: "reduce", calculation: "median" }],
      [{ kind: "reduce", calculation: "mean", pattern: "a" }],
      [{ kind: "group_by" }],
      [{ kind: "group_by", group_by: "label", calculation: "sum" }],
      [{ kind: "group_by", group_by: "metric", calculation: "delta" }],
      [{ kind: "group_by", group_by: "metric", calculation: "sum", pattern: "a" }],
      [{ kind: "rename_by_regex", pattern: "([", replacement: "x" }],
      [{ kind: "rename_by_regex", pattern: "a".repeat(129), replacement: "x" }],
      [{ kind: "filter_by_value", comparison: "above", value: 1 }],
      [{ kind: "filter_by_value", comparison: "gt" }],
      [{ kind: "sort_by", statistic: "last", order: "up" }],
      [{ kind: "limit", count: 9 }],
      [{ kind: "limit", count: 2, pattern: "a" }],
      [{ kind: "binary_operation", operator: "+", right: "b" }],
      [{ kind: "binary_operation", left: "a", operator: "%", right: "b" }],
      [{ kind: "binary_operation", left: "/(/", operator: "+", right: "b" }],
      [{ kind: "binary_operation", left: "a", operator: "+", right: "b", pattern: "x" }],
      Array.from({ length: 5 }, () => ({ kind: "limit", count: 1 })),
    ]
    for (const transformations of cases) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], transformations }] })).toThrow("workspace.import_invalid")
    }
  })
})

describe("panel overrides", () => {
  it("round-trips field overrides and drops an empty list", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], overrides: [
      { matcher: { type: "by_regex", value: "^node-" }, color: "rose" },
      { matcher: { type: "by_name", value: "CPU" }, unit: "percent", hide_legend: true },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(imported({ ...definition(), panels: [{ ...definition().panels[0], overrides: [] }] }).panels[0])
      .not.toHaveProperty("overrides")
  })

  it("round-trips the per series line width and fill opacity and refuses out of range values", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], overrides: [
      { matcher: { type: "by_name", value: "CPU" }, line_width: 4, fill_opacity: 0 },
      { matcher: { type: "by_regex", value: "." }, fill_opacity: 100 },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    for (const override of [
      { matcher: { type: "by_name", value: "a" }, line_width: 0 },
      { matcher: { type: "by_name", value: "a" }, line_width: 5 },
      { matcher: { type: "by_name", value: "a" }, line_width: 2.5 },
      { matcher: { type: "by_name", value: "a" }, fill_opacity: -1 },
      { matcher: { type: "by_name", value: "a" }, fill_opacity: 101 },
      { matcher: { type: "by_name", value: "a" }, fill_opacity: "20" },
    ]) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], overrides: [override] }] })).toThrow("workspace.import_invalid")
    }
    // 柱状图不画线宽，服务端会拒绝，这里同样按无效文件处理。
    const barChart = { ...definition().panels[0], type: "barchart" as const, overrides: [
      { matcher: { type: "by_name" as const, value: "a" }, line_width: 2 },
    ] }
    expect(() => imported({ ...definition(), panels: [barChart] })).toThrow("workspace.import_invalid")
  })

  it("round-trips the per series axis options and refuses unusable ones", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], overrides: [
      { matcher: { type: "by_name", value: "CPU" }, axis_label: "CPU %", axis_soft_min: 0, axis_soft_max: 100 },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const override = (value: Record<string, unknown>) => ({ ...definition().panels[0], overrides: [{ matcher: { type: "by_name", value: "CPU" }, ...value }] })
    for (const value of [{ axis_label: "" }, { axis_label: "a".repeat(41) }, { axis_soft_min: 90, axis_soft_max: 10 }, { axis_soft_min: "0" }]) {
      expect(() => imported({ ...definition(), panels: [override(value)] })).toThrow("workspace.import_invalid")
    }
    // 面板级固定上下限会让逐序列柔性值永远不生效，导入侧同样按无效文件处理。
    const fixed = { ...override({ axis_soft_min: 0 }), min: 0 }
    expect(() => imported({ ...definition(), panels: [fixed] })).toThrow("workspace.import_invalid")
    // 柱状图没有逐序列轴标题/柔性上下限的渲染路径（服务端拒绝 stat 等无值轴面板）。
    const stat = override({ axis_label: "CPU" })
    expect(() => imported({ ...definition(), panels: [{ ...stat, type: "stat" as const }] })).toThrow("workspace.import_invalid")
  })

  it("round-trips the standard options and refuses the ones the panel cannot render", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "gauge" as const, overrides: [
      { matcher: { type: "by_value", value: "0" }, display_name: "CPU idle", decimals: 1, min: 0, max: 90 },
      { matcher: { type: "by_type", value: "percent" }, color: "rose" },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const override = (value: Record<string, unknown>, type = "gauge") =>
      ({ ...definition().panels[0], type: type as "gauge", overrides: [{ matcher: { type: "by_name", value: "CPU" }, ...value }] })
    for (const value of [{ display_name: "x".repeat(129) }, { decimals: 7 }, { decimals: -1 }, { min: 90, max: 90 },
      { min: "0" }, { decimals: 1.5 }, { min: 0, max: 90, axis_soft_min: 0 }]) {
      expect(() => imported({ ...definition(), panels: [override(value)] })).toThrow("workspace.import_invalid")
    }
    // 逐序列值域只属于有单值量程的面板；显示名与小数位只属于会给序列渲染名字与数值的面板。
    expect(() => imported({ ...definition(), panels: [override({ min: 0, max: 90 }, "table")] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [override({ display_name: "CPU" }, "heatmap")] })).toThrow("workspace.import_invalid")
    // 两种新匹配器：类型必须落在枚举里，值必须能解析成有限数。
    for (const matcher of [{ type: "by_type", value: "gigabytes" }, { type: "by_value", value: "zero" }]) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], overrides: [{ matcher, color: "rose" }] }] }))
        .toThrow("workspace.import_invalid")
    }
  })

  it("round-trips the panel no value text and refuses it where nothing renders a placeholder", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "stat" as const, no_value: "n/a" }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    expect(imported({ ...definition(), panels: [{ ...definition().panels[0], no_value: "" }] }).panels[0]).not.toHaveProperty("no_value")
    const panel = { ...definition().panels[0], no_value: "n/a" }
    expect(() => imported({ ...definition(), panels: [panel] })).toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...panel, type: "stat" as const, no_value: "x".repeat(33) }] }))
      .toThrow("workspace.import_invalid")
    expect(() => imported({ ...definition(), panels: [{ ...panel, type: "stat" as const, no_value: 7 }] })).toThrow("workspace.import_invalid")
    expect(imported({ ...definition(), panels: [{ ...panel, type: "stat" as const }] }).panels[0].no_value).toBe("n/a")
  })

  it("refuses unknown matchers, unusable properties and rows that cannot render a series", () => {
    const cases = [
      [{ matcher: { type: "by_label", value: "a" }, color: "rose" }],
      [{ matcher: { type: "by_name", value: "" }, color: "rose" }],
      [{ matcher: { type: "by_regex", value: "([" }, color: "rose" }],
      [{ matcher: { type: "by_name", value: "a" } }],
      [{ matcher: { type: "by_name", value: "a" }, color: "chartreuse" }],
      [{ matcher: { type: "by_name", value: "a" }, unit: "u".repeat(33) }],
      [{ matcher: { type: "by_name", value: "a" }, color: "rose", label: "CPU" }],
      [{ matcher: { type: "by_name", value: "a", extra: 1 }, color: "rose" }],
      [{ matcher: { type: "by_name", value: "a" }, axis_placement: "centre" }],
      [{ matcher: { type: "by_name", value: "a" }, axis_placement: true }],
      Array.from({ length: 17 }, () => ({ matcher: { type: "by_name", value: "a" }, color: "rose" })),
    ]
    for (const overrides of cases) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], overrides }] })).toThrow("workspace.import_invalid")
    }
    const panel = { ...definition().panels[0], overrides: [{ matcher: { type: "by_name", value: "a" }, color: "rose" }] }
    for (const type of ["row", "text"]) {
      expect(() => imported({ ...definition(), panels: [{ ...panel, type }] })).toThrow("workspace.import_invalid")
    }
  })
})

describe("panel fill below to", () => {
  it("round-trips the fill target on unstacked time series panels", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], overrides: [
      { matcher: { type: "by_name", value: "CPU min" }, fill_below_to: "CPU max" },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("refuses a target the server would reject or ignore", () => {
    const overrides = (fill: unknown) => [{ matcher: { type: "by_name", value: "CPU min" }, fill_below_to: fill }]
    for (const fill of ["", 12, true]) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], overrides: overrides(fill) }] }))
        .toThrow("workspace.import_invalid")
    }
    // 堆叠面板没有填充带，柱状图也没有折线：两者都按无效文件处理。
    const stacked = { ...definition().panels[0], stack: "normal" as const, overrides: overrides("CPU max") }
    expect(() => imported({ ...definition(), panels: [stacked] })).toThrow("workspace.import_invalid")
    const barChart = { ...definition().panels[0], type: "barchart" as const, overrides: overrides("CPU max") }
    expect(() => imported({ ...definition(), panels: [barChart] })).toThrow("workspace.import_invalid")
  })
})

describe("panel override thresholds", () => {
  it("round-trips a per series threshold band on panels that colour by threshold", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "stat", overrides: [
      { matcher: { type: "by_ref_id", value: "a" }, thresholds: [{ value: 0, color: "green" }, { value: 90, color: "rose" }] },
    ] }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })

  it("refuses a band the server would reject or ignore", () => {
    const overrides = (thresholds: unknown) => [{ matcher: { type: "by_name", value: "CPU" }, thresholds }]
    for (const thresholds of [
      [],
      "green",
      [{ value: 10, color: "green" }, { value: 5, color: "rose" }],
      [{ value: 10, color: "green" }, { value: 10, color: "rose" }],
      Array.from({ length: 5 }, (_, index) => ({ value: index * 10, color: "green" })),
      [{ value: 10, color: "chartreuse" }],
      [{ value: "10", color: "green" }],
      [{ color: "green" }],
      [10],
    ]) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], type: "stat", overrides: overrides(thresholds) }] }))
        .toThrow("workspace.import_invalid")
    }
    // 折线只按序列色渲染，服务端会拒绝逐序列阈值，导入同样按无效文件处理。
    expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], overrides: overrides([{ value: 10, color: "green" }]) }] }))
      .toThrow("workspace.import_invalid")
  })
})

describe("panel series axis placement", () => {
  it("round-trips a per-series axis placement on both switchable axis panels", () => {
    for (const type of ["timeseries", "barchart"] as const) {
      const source = definition()
      source.panels[0] = { ...source.panels[0], type,
        overrides: [{ matcher: { type: "by_name", value: "Memory usage" }, axis_placement: "right" }] }
      expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    }
  })

  it("refuses the placement on panels without a second axis", () => {
    const overrides = [{ matcher: { type: "by_name", value: "CPU" }, axis_placement: "right" }]
    for (const type of ["table", "stat", "heatmap"]) {
      expect(() => imported({ ...definition(), panels: [{ ...definition().panels[0], type, overrides }] })).toThrow("workspace.import_invalid")
    }
  })
})

describe("panel line style", () => {
  it("round-trips line width, fill, points and stacking", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], line_width: 4, fill_opacity: 30, show_points: "always", stack: "normal" }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const plain = definition()
    expect(importDashboardJSON(exportDashboardJSON(plain)).panels[0]).not.toHaveProperty("line_width")
    expect(importDashboardJSON(exportDashboardJSON(plain)).panels[0]).not.toHaveProperty("stack")
  })

  it("round-trips the fill gradient", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], fill_opacity: 25, gradient_mode: "hue" }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const bar = definition()
    bar.panels[0] = { ...bar.panels[0], type: "barchart", gradient_mode: "opacity" }
    expect(importDashboardJSON(exportDashboardJSON(bar))).toEqual(bar)
  })

  it("round-trips the line interpolation", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], line_interpolation: "step_before" }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const plain = definition()
    expect(importDashboardJSON(exportDashboardJSON(plain)).panels[0]).not.toHaveProperty("line_interpolation")
  })

  it("refuses unknown or bar-only interpolations", () => {
    const source = definition()
    for (const change of [{ line_interpolation: "cubic" }, { line_interpolation: "stepBefore" }]) {
      const panel = { ...source.panels[0], ...change }
      expect(() => imported({ ...source, panels: [panel] })).toThrow("workspace.import_invalid")
    }
    const bar = { ...source.panels[0], type: "barchart", line_interpolation: "smooth" }
    expect(() => imported({ ...source, panels: [bar] })).toThrow("workspace.import_invalid")
  })

  it("refuses unknown gradients or a gradient without fill", () => {
    const source = definition()
    const cases = [{ gradient_mode: "scheme" }, { gradient_mode: "hue" }, { gradient_mode: "opacity", fill_opacity: 0 }]
    for (const change of cases) {
      const panel = { ...source.panels[0], ...change }
      expect(() => imported({ ...source, panels: [panel] })).toThrow("workspace.import_invalid")
    }
    const stat = { ...source.panels[0], type: "stat", fill_opacity: 30, gradient_mode: "hue" }
    expect(() => imported({ ...source, panels: [stat] })).toThrow("workspace.import_invalid")
  })

  it("refuses out of range or non-timeseries line styles", () => {
    const source = definition()
    for (const change of [{ line_width: 0 }, { line_width: 5 }, { fill_opacity: 101 }, { show_points: "sometimes" }, { stack: "cumulative" }, { stack: "percent", scale: "log" }, { line_width: 2.5 }]) {
      const panel = { ...source.panels[0], ...change }
      expect(() => imported({ ...source, panels: [panel] })).toThrow("workspace.import_invalid")
    }
    const stat = { ...source.panels[0], type: "stat", stack: "normal" }
    expect(() => imported({ ...source, panels: [stat] })).toThrow("workspace.import_invalid")
  })
})

describe("panel bar gauge", () => {
  it("round-trips orientation, display and the unfilled toggle", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "bargauge", bar_orientation: "horizontal", bar_display: "lcd", bar_unfilled: false }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const plain = definition()
    expect(importDashboardJSON(exportDashboardJSON(plain)).panels[0]).not.toHaveProperty("bar_display")
  })

  it("refuses unknown enums, non-boolean toggles and non-bar-gauge panels", () => {
    const source = definition()
    for (const change of [{ bar_orientation: "diagonal" }, { bar_display: "neon" }, { bar_unfilled: "no" }]) {
      const panel = { ...source.panels[0], type: "bargauge", ...change }
      expect(() => imported({ ...source, panels: [panel] })).toThrow("workspace.import_invalid")
    }
    const stat = { ...source.panels[0], type: "stat", bar_display: "basic" }
    expect(() => imported({ ...source, panels: [stat] })).toThrow("workspace.import_invalid")
  })

  it("accepts a bar gauge with several series", () => {
    const source = definition()
    const series = source.panels[0].series[0]
    source.panels[0] = { ...source.panels[0], type: "bargauge", series: [series, { ...series, ref_id: "b", color: "green" }] }
    expect(importDashboardJSON(exportDashboardJSON(source)).panels[0].series).toHaveLength(2)
  })
})

describe("panel pie chart", () => {
  it("round-trips the pie type and label mode", () => {
    const source = definition()
    source.panels[0] = { ...source.panels[0], type: "piechart", pie_chart_type: "donut", pie_label_mode: "both" }
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const plain = definition()
    expect(importDashboardJSON(exportDashboardJSON(plain)).panels[0]).not.toHaveProperty("pie_chart_type")
  })

  it("refuses unknown enums and pie fields on other panel types", () => {
    const source = definition()
    for (const change of [{ pie_chart_type: "radar" }, { pie_label_mode: "legend" }]) {
      const panel = { ...source.panels[0], type: "piechart", ...change }
      expect(() => imported({ ...source, panels: [panel] })).toThrow("workspace.import_invalid")
    }
    const gauge = { ...source.panels[0], type: "gauge", pie_chart_type: "donut" }
    expect(() => imported({ ...source, panels: [gauge] })).toThrow("workspace.import_invalid")
  })
})

describe("dashboard search tags", () => {
  it("keeps tags through export and import and omits them when unused", () => {
    const source = definition({ tags: ["prod", "hk-1"] })
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const untagged = imported({ version: 1, title: "Draft", panels: [], variables: [] })
    expect(untagged).not.toHaveProperty("tags")
    expect(importDashboardJSON(exportDashboardJSON(untagged))).not.toHaveProperty("tags")
  })

  it("refuses malformed or unbounded tags", () => {
    for (const tags of [["Prod"], ["a".repeat(65)], ["prod", "prod"], [1], Array.from({ length: 9 }, (_, index) => `t${index}`)]) {
      expect(() => imported({ version: 1, title: "D", panels: [], variables: [], tags })).toThrow("workspace.import_invalid")
    }
  })
})

describe("read-only link hiding", () => {
  it("keeps the switch through export and import, and omits it when unused", () => {
    const hidden = { ...definition(), hide_links: true }
    expect(importDashboardJSON(exportDashboardJSON(hidden))).toEqual(hidden)
    expect(imported({ version: 1, title: "Draft", panels: [], variables: [] })).not.toHaveProperty("hide_links")
    expect(imported({ version: 1, title: "Draft", panels: [], variables: [], hide_links: false })).not.toHaveProperty("hide_links")
  })

  it("refuses a non-boolean switch", () => {
    for (const value of ["true", 1, null]) {
      expect(() => imported({ version: 1, title: "Draft", panels: [], variables: [], hide_links: value })).toThrow("workspace.import_invalid")
    }
  })
})

describe("dashboard portability", () => {
  it("round-trips an exported dashboard", () => {
    const source = definition()
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
  })
  it("accepts a bare definition and a bundle envelope", () => {
    const source = definition()
    expect(imported(source)).toEqual(source)
    expect(imported({ kind: "beat.dashboard", version: 1, definition: source })).toEqual(source)
  })
  it("rejects malformed payloads", () => {
    for (const payload of ["not json", "[]", JSON.stringify({ version: 2, panels: [], variables: [] }),
      JSON.stringify({ version: 1, title: "x" }), JSON.stringify({ version: 1, panels: [], variables: [], title: "  " })]) {
      expect(() => importDashboardJSON(payload)).toThrow("workspace.import_invalid")
    }
  })
  it("normalizes optional fields to bounded defaults", () => {
    const value = imported({ version: 1, title: "Draft", panels: [], variables: [], time_range_seconds: 10, refresh_seconds: 7, description: 5 })
    expect(value).toMatchObject({ description: "", time_range_seconds: 3600, refresh_seconds: 15, panels: [] })
    expect(imported({ version: 1, title: "Draft", panels: [], variables: [], time_range_seconds: 2678400, refresh_seconds: 0 }))
      .toMatchObject({ time_range_seconds: 2678400, refresh_seconds: 0 })
  })
  it("rejects unbounded variables", () => {
    const variables = [1, 2, 3, 4, 5].map((index) => ({ name: `node${index}`, type: "node", label: `Node ${index}` }))
    expect(() => imported({ version: 1, title: "D", panels: [], variables })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", panels: [], variables: [{ name: "Node", type: "node", label: "Node" }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", panels: [], variables: [{ name: "__proto__", type: "node", label: "Node" }] })).toThrow("workspace.import_invalid")
  })
  it("rejects unsafe panels and series", () => {
    const panel = definition().panels[0]
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, id: "constructor" }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, series: [] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: [], panels: [{ ...panel, series: [{ ...panel.series[0], node_id: "$missing" }] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, type: "gauge", series: [...panel.series, { ...panel.series[0], ref_id: "b" }] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, series: [{ ...panel.series[0], aggregation: "median" }] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, series: [{ ...panel.series[0], color: "magenta" }] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, thresholds: [{ value: 90, color: "rose" }, { value: 20, color: "rose" }] }] })).toThrow("workspace.import_invalid")
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, min: 5, max: 1 }] })).toThrow("workspace.import_invalid")
  })
  it("rejects duplicate panels and excessive counts", () => {
    const panel = definition().panels[0]
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [panel, panel] })).toThrow("workspace.import_invalid")
    const panels = Array.from({ length: 25 }, (_, index) => ({ ...panel, id: `p${index}` }))
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels })).toThrow("workspace.import_invalid")
    const series = Array.from({ length: 9 }, (_, index) => ({ ...panel.series[0], ref_id: `s${index}` }))
    expect(() => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, series }] })).toThrow("workspace.import_invalid")
  })
  it("keeps axis, unit and legend overrides that the server accepts", () => {
    const panel = definition().panels[0]
    const value = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, legend: false, unit: "bytes", decimals: 2, min: 0, max: 100 }] })
    expect(value.panels[0]).toMatchObject({ legend: false, unit: "bytes", decimals: 2, min: 0, max: 100 })
  })
  it("round-trips the soft axis limits the server accepts", () => {
    const panel = definition().panels[0]
    const value = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, soft_min: -10, soft_max: 90 }] })
    expect(value.panels[0]).toMatchObject({ soft_min: -10, soft_max: 90 })
    expect(JSON.parse(exportDashboardJSON(value)).definition.panels[0]).toMatchObject({ soft_min: -10, soft_max: 90 })
    const lone = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, type: "barchart", min: 0, soft_max: 90 }] })
    expect(lone.panels[0]).toMatchObject({ soft_max: 90 })
    expect(lone.panels[0]).not.toHaveProperty("min")
  })
  it("round-trips the logarithmic axis scale the server accepts", () => {
    const panel = definition().panels[0]
    const value = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, type: "barchart", scale: "log", min: 1, max: 1000 }] })
    expect(value.panels[0]).toMatchObject({ scale: "log", min: 1, max: 1000 })
    expect(JSON.parse(exportDashboardJSON(value)).definition.panels[0]).toMatchObject({ scale: "log" })
    // linear 与未声明等效：导入按缺省收敛，不留字段。
    const linear = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, scale: "linear" }] })
    expect(linear.panels[0]).not.toHaveProperty("scale")
  })
  it("round-trips a bounded axis label", () => {
    const panel = definition().panels[0]
    const value = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, axis_label: "CPU 使用率" }] })
    expect(value.panels[0]).toMatchObject({ axis_label: "CPU 使用率" })
    expect(JSON.parse(exportDashboardJSON(value)).definition.panels[0]).toMatchObject({ axis_label: "CPU 使用率" })
    expect(imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, axis_label: "" }] }).panels[0]).not.toHaveProperty("axis_label")
  })
  it("round-trips the axis display options and drops their defaults", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    const hidden = importedWith({ grid: false, axis_placement: "right" })
    expect(hidden.panels[0]).toMatchObject({ grid: false, axis_placement: "right" })
    expect(JSON.parse(exportDashboardJSON(hidden)).definition.panels[0]).toMatchObject({ grid: false, axis_placement: "right" })
    // grid true 与 left 都是缺省值：导入按缺省收敛，不留字段。
    const defaults = importedWith({ grid: true, axis_placement: "left" })
    expect(defaults.panels[0]).not.toHaveProperty("grid")
    expect(defaults.panels[0]).not.toHaveProperty("axis_placement")
  })
  it("round-trips centered zero and refuses the conflicting combinations", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    const centered = importedWith({ centered_zero: true, soft_max: 40 })
    expect(centered.panels[0]).toMatchObject({ centered_zero: true, soft_max: 40 })
    expect(JSON.parse(exportDashboardJSON(centered)).definition.panels[0]).toMatchObject({ centered_zero: true })
    // false 是缺省值：导入按缺省收敛。
    expect(importedWith({ centered_zero: false }).panels[0]).not.toHaveProperty("centered_zero")
    expect(() => importedWith({ centered_zero: "yes" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ centered_zero: true, min: 0, max: 100 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "stat", centered_zero: true })).toThrow("workspace.import_invalid")
  })
  it("refuses axis display options the server would reject", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(() => importedWith({ grid: "no" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ axis_placement: "top" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "stat", grid: false })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "table", axis_placement: "right" })).toThrow("workspace.import_invalid")
  })
  it("refuses axis labels the server would reject", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(() => importedWith({ axis_label: "a".repeat(41) })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ axis_label: "CPU\nusage" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ axis_label: 7 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "stat", axis_label: "CPU" })).toThrow("workspace.import_invalid")
    expect(importedWith({ type: "barchart", axis_label: "bytes" }).panels[0]).toMatchObject({ axis_label: "bytes" })
  })
  it("refuses axis scales the server would reject", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(() => importedWith({ scale: "log2" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "stat", scale: "log" })).toThrow("workspace.import_invalid")
    // 固定上下限只有同时出现才会被导入保留，因此非正下限要连写上界一起验证。
    expect(() => importedWith({ scale: "log", min: 0, max: 100 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ scale: "log", soft_min: -5 })).toThrow("workspace.import_invalid")
    expect(importedWith({ scale: "log", min: 1, max: 100 }).panels[0]).toMatchObject({ scale: "log", min: 1, max: 100 })
  })
  it("refuses soft axis limits the server would reject", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(() => importedWith({ soft_min: "0" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ soft_min: 90, soft_max: 10 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ soft_min: 10, soft_max: 10 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ type: "stat", soft_min: 0 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ min: 0, max: 100, soft_min: -10 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ min: 0, max: 100, soft_max: 90 })).toThrow("workspace.import_invalid")
  })
  it("round-trips legend values, order and tooltip", () => {
    const panel = definition().panels[0]
    const value = imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, legend_values: ["last", "mean"], legend_sort: "desc", tooltip: "single" }] })
    expect(value.panels[0]).toMatchObject({ legend_values: ["last", "mean"], legend_sort: "desc", tooltip: "single" })
    expect(JSON.parse(exportDashboardJSON(value)).definition.panels[0]).toMatchObject({ legend_values: ["last", "mean"], legend_sort: "desc", tooltip: "single" })
    const defaults = imported({ version: 1, title: "D", variables: definition().variables, panels: [panel] })
    expect(defaults.panels[0]).not.toHaveProperty("legend_values")
    expect(defaults.panels[0]).not.toHaveProperty("legend_sort")
    expect(defaults.panels[0]).not.toHaveProperty("tooltip")
  })
  it("rejects unbounded or hidden legend settings", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(importedWith({ legend_values: ["last", "min", "max", "mean"] }).panels[0]).toMatchObject({ legend_values: ["last", "min", "max", "mean"] })
    expect(() => importedWith({ legend_values: ["p95"] })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend_values: ["last", "last"] })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend_values: ["last", "min", "max", "mean", "last"] })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend_values: "last" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend_sort: "median" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ tooltip: "multi" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend: false, legend_values: ["last"] })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ legend: false, legend_sort: "asc" })).toThrow("workspace.import_invalid")
    expect(importedWith({ legend: false, legend_sort: "none", tooltip: "off" }).panels[0]).toMatchObject({ legend: false, tooltip: "off" })
  })
  it("round-trips the panel time override and refuses an unbounded window", () => {
    const panel = definition().panels[0]
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    const value = importedWith({ time_from: "now-6h", time_to: "now" })
    expect(value.panels[0]).toMatchObject({ time_from: "now-6h", time_to: "now" })
    expect(JSON.parse(exportDashboardJSON(value)).definition.panels[0]).toMatchObject({ time_from: "now-6h", time_to: "now" })
    expect(importedWith({ time_from: "now-6h" }).panels[0]).toMatchObject({ time_from: "now-6h" })
    expect(importedWith({ time_from: "now-6h" }).panels[0]).not.toHaveProperty("time_to")
    expect(importedWith({ time_from: "", time_to: "" }).panels[0]).not.toHaveProperty("time_from")
    expect(() => importedWith({ time_to: "now" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ time_from: "now-40d" })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ time_from: "6h" })).toThrow("workspace.import_invalid")
  })
  it("round-trips dashboard query defaults and refuses values outside the budget", () => {
    const source = definition({ max_points: 240, now_delay_seconds: 30 })
    expect(importDashboardJSON(exportDashboardJSON(source))).toEqual(source)
    const importedWith = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [], ...patch })
    expect(importedWith({ max_points: 2 }).max_points).toBe(2)
    expect(importedWith({ now_delay_seconds: 0 }).now_delay_seconds).toBe(0)
    expect(importedWith({ max_points: 600 }).max_points).toBe(600)
    expect(importedWith({})).not.toHaveProperty("max_points")
    expect(() => importedWith({ max_points: 1 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ max_points: 601 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ max_points: 12.5 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ now_delay_seconds: -1 })).toThrow("workspace.import_invalid")
    expect(() => importedWith({ now_delay_seconds: 3601 })).toThrow("workspace.import_invalid")
    const panel = definition().panels[0]
    const withPanel = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(withPanel({ max_points: 90 }).panels[0]).toMatchObject({ max_points: 90 })
    expect(withPanel({}) .panels[0]).not.toHaveProperty("max_points")
    expect(() => withPanel({ max_points: 601 })).toThrow("workspace.import_invalid")
  })
  it("round-trips the panel time shift and refuses a malformed duration", () => {
    const panel = definition().panels[0]
    const withPanel = (patch: Record<string, unknown>) => imported({ version: 1, title: "D", variables: definition().variables, panels: [{ ...panel, ...patch }] })
    expect(withPanel({ time_shift: "1d" }).panels[0]).toMatchObject({ time_shift: "1d" })
    expect(JSON.parse(exportDashboardJSON(withPanel({ time_shift: "12h" }))).definition.panels[0]).toMatchObject({ time_shift: "12h" })
    expect(withPanel({}) .panels[0]).not.toHaveProperty("time_shift")
    expect(withPanel({ time_shift: "" }).panels[0]).not.toHaveProperty("time_shift")
    for (const shift of ["0s", "1w", "32d", "now-1d"]) expect(() => withPanel({ time_shift: shift })).toThrow("workspace.import_invalid")
  })
  it("builds shareable dashboard links", () => {
    expect(workspaceShareLink({ origin: "https://beat.example", pathname: "/admin/metrics" }, "admin-default", "node-1"))
      .toBe("https://beat.example/admin/metrics?dashboard=admin-default&node_id=node-1")
    expect(workspaceShareLink({ origin: "https://beat.example", pathname: "/admin/metrics" }, "new")).toBe("https://beat.example/admin/metrics")
    expect(workspaceShareLink({ origin: "https://beat.example", pathname: "/admin/metrics" }, "dash 1")).toBe("https://beat.example/admin/metrics?dashboard=dash+1")
  })
})
