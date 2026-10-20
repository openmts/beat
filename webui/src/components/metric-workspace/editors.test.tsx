import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
// 这两个交互用例在满载并行门禁下会超过默认 5 秒上限，显式放宽超时以保持门禁可重复。
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { WorkspacePanelEditor } from "./panel-editor"
import { WorkspaceDefinitionEditor } from "./definition-editor"
import { WorkspaceTimeControls } from "./time-controls"
import { workspaceDashboard, workspaceMetric, workspaceNode } from "@/test/metric-workspace-fixtures"

async function choose(user: ReturnType<typeof userEvent.setup>, label: string, value: string) {
  await user.click(screen.getByRole("combobox", { name: label }))
  await user.click(await screen.findByRole("option", { name: value }))
}

describe("workspace editors", () => {
  it("edits multiple queries and rejects an ambiguous gauge", { timeout: 20000 }, async () => {
    const user = userEvent.setup()
    const apply = vi.fn()
    const dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric(), workspaceMetric("memory")]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add query" }))
    await choose(user, "Metric B", "Memory usage")
    await choose(user, "Node B", "Production host")
    await choose(user, "Aggregation B", "Maximum")
    fireEvent.change(screen.getByRole("textbox", { name: "Legend label B" }), { target: { value: "Resident memory" } })
    await choose(user, "Color B", "Green")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ series: expect.arrayContaining([expect.objectContaining({ ref_id: "B", metric: "memory", node_id: "node-one", aggregation: "max", label: "Resident memory", color: "green" })]) }))
    apply.mockClear()
    await choose(user, "Visualization", "Gauge")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Stat and gauge panels require one query")
    await user.click(screen.getByRole("button", { name: "Remove query B" }))
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "gauge" }))
    expect(apply.mock.calls.at(-1)?.[0].series).toHaveLength(1)
  })

  it("edits Grafana-style value mappings on a stat panel", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    const panel = { ...dashboard.definition.panels[0], type: "stat" as const }
    render(<LocaleProvider><WorkspacePanelEditor panel={panel} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.queryByText("Line style")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add value mapping" }))
    fireEvent.change(screen.getByLabelText("Mapped value 1"), { target: { value: "40" } })
    fireEvent.change(screen.getByLabelText("Display text 1"), { target: { value: "Healthy" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ value_mappings: [{ type: "value", value: 40, text: "Healthy", color: "rose" }] }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Display text 1"), { target: { value: "x".repeat(33) } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findAllByText("Display text is limited to 32 characters.")).length).toBeGreaterThan(0)
    expect(apply).not.toHaveBeenCalled()
  })

  it("edits a Grafana state timeline panel", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await choose(user, "Visualization", "State timeline")
    expect(await screen.findByText("State timeline", { selector: "legend" })).toBeInTheDocument()
    await choose(user, "Show values", "Always")
    await user.click(screen.getByRole("switch", { name: "Merge equal values" }))
    // 状态时间线接受值映射：切到该类型后编辑器仍然给出映射入口。
    expect(screen.getByRole("button", { name: "Add value mapping" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "statetimeline", timeline_show_values: "always", timeline_merge: false }))
    apply.mockClear()
    // 切回折线时状态时间线选项必须被清理，否则服务端会拒绝保存。
    await choose(user, "Visualization", "Time series")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "timeseries", timeline_show_values: undefined, timeline_merge: undefined }))
  })

  it("edits a geo map location table and clears its fields on the next type", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await choose(user, "Visualization", "Geo map")
    expect(await screen.findByText("Geo map", { selector: "legend" })).toBeInTheDocument()
    // 切到地理地图会预置一条空地点：匹配串没填时必须拦住保存并给出定位错误。
    await user.click(screen.getByRole("button", { name: "Apply" }))
    // 选项区与页脚会各渲染一条同文案的提示，因此按集合断言。
    expect((await screen.findAllByRole("alert")).map((node) => node.textContent)).toContain("Location matches must be unique and carry valid coordinates.")
    expect(apply).not.toHaveBeenCalled()
    await user.type(screen.getByLabelText("Node, device or name 1"), "node-one")
    await user.clear(screen.getByLabelText("Latitude 1"))
    await user.type(screen.getByLabelText("Latitude 1"), "39.9")
    await user.clear(screen.getByLabelText("Longitude 1"))
    await user.type(screen.getByLabelText("Longitude 1"), "116.4")
    await user.click(screen.getByRole("button", { name: "Add location" }))
    await user.type(screen.getByLabelText("Node, device or name 2"), "node-two")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "geomap",
      geo_locations: [expect.objectContaining({ match: "node-one", latitude: 39.9, longitude: 116.4 }), expect.objectContaining({ match: "node-two" })] }))
    apply.mockClear()
    // 切回折线时地理地图专属字段必须被清理，否则服务端会拒绝保存。
    await choose(user, "Visualization", "Time series")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "timeseries", geo_locations: undefined, geo_layer: undefined, geo_zoom: undefined }))
  })

  it("edits a Grafana histogram panel and clears its fields on the next type", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await choose(user, "Visualization", "Histogram")
    expect(await screen.findByText("Histogram", { selector: "legend" })).toBeInTheDocument()
    // 分桶输入框只回显显式取值，清空后以缺省值 12 兜底，因此可以像 Grafana 一样清空重填。
    const buckets = screen.getByLabelText("Value buckets")
    await user.clear(buckets)
    await user.type(buckets, "24")
    await choose(user, "Count scale", "Logarithmic")
    await user.click(screen.getByRole("switch", { name: "Combine series" }))
    // 直方图与热力图、折线一样不接受值映射：切到该类型后映射入口必须消失。
    expect(screen.queryByRole("button", { name: "Add value mapping" })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "histogram", histogram_buckets: 24, histogram_scale: "log", histogram_merge: true }))
    apply.mockClear()
    // 切回折线时直方图专属字段必须被清理，否则服务端会拒绝保存。
    await choose(user, "Visualization", "Time series")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "timeseries", histogram_buckets: undefined, histogram_scale: undefined, histogram_merge: undefined }))
  })

  it("edits a Grafana-style transformation chain and blocks invalid steps", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.getByText("Transformations")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add transformation" }))
    await choose(user, "Step 1", "Sort by")
    await choose(user, "Order 1", "Lowest first")
    await user.click(screen.getByRole("button", { name: "Add transformation" }))
    await choose(user, "Step 2", "Limit")
    fireEvent.change(screen.getByLabelText("Keep 2"), { target: { value: "3" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ transformations: [
      { kind: "sort_by", statistic: "last", order: "asc" }, { kind: "limit", count: 3 },
    ] }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Keep 2"), { target: { value: "9" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findAllByText("Keep between 1 and 8 series.")).length).toBeGreaterThan(0)
    expect(apply).not.toHaveBeenCalled()
  })

  it("edits a Grafana-style field override and blocks an empty rule", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.getByText("Overrides")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add override" }))
    // 新增覆盖带一个可用起点：按正则匹配 ^node- 且改成绿色。
    await choose(user, "Match by 1", "Series name")
    fireEvent.change(screen.getByLabelText("Match value 1"), { target: { value: "CPU" } })
    await choose(user, "Colour 1", "Rose")
    fireEvent.change(screen.getByLabelText("Unit 1"), { target: { value: "bytes" } })
    await user.click(screen.getByRole("checkbox", { name: "Hide from legend" }))
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_name", value: "CPU" }, color: "rose", unit: "bytes", hide_legend: true },
    ] }))
    apply.mockClear()
    // 清空颜色、单位与图例开关后这条覆盖什么都不改，必须拦下而不是保存出无声配置。
    await choose(user, "Colour 1", "Keep current")
    fireEvent.change(screen.getByLabelText("Unit 1"), { target: { value: "" } })
    await user.click(screen.getByRole("checkbox", { name: "Hide from legend" }))
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findAllByText("Every override needs a colour, a unit or the legend switch.")).length).toBeGreaterThan(0)
    expect(apply).not.toHaveBeenCalled()
  })

  it("edits the standard options override and the matcher value by panel type", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    const panel = { ...dashboard.definition.panels[0], type: "gauge" as const, unit: "percent" as const }
    render(<LocaleProvider><WorkspacePanelEditor panel={panel} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add override" }))
    // 按字段类型时匹配值换成单位族下拉，避免在自由文本里写出永远不命中的类型。
    await choose(user, "Match by 1", "Series type")
    await choose(user, "Match value 1", "bytes")
    fireEvent.change(screen.getByLabelText("Display name 1"), { target: { value: "CPU idle" } })
    fireEvent.change(screen.getByLabelText("Decimal places 1"), { target: { value: "1" } })
    fireEvent.change(screen.getByLabelText("Min 1"), { target: { value: "0" } })
    fireEvent.change(screen.getByLabelText("Max 1"), { target: { value: "90" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_type", value: "bytes" }, color: "green", display_name: "CPU idle", decimals: 1, min: 0, max: 90 },
    ] }))
    // 面板级 No value 文本只在会为缺失样本渲染一行文本的面板上出现。
    fireEvent.change(screen.getByLabelText("No value"), { target: { value: "n/a" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply.mock.calls.at(-1)?.[0]).toMatchObject({ no_value: "n/a" })
  })

  it("hides the standard options override on panels that render no series value", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    // 折线面板没有单值量程：Min/Max 与占位文本都不显示，显示名与小数位仍然可用。
    expect(screen.queryByLabelText("No value")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add override" }))
    expect(screen.getByLabelText("Display name 1")).toBeInTheDocument()
    expect(screen.queryByLabelText("Min 1")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add override" }))
    await choose(user, "Match by 2", "Sample value")
    fireEvent.change(screen.getByLabelText("Match value 2"), { target: { value: "0" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    const lastPanel = apply.mock.calls.at(-1)?.[0]
    expect(lastPanel?.overrides?.[1]).toMatchObject({ matcher: { type: "by_value", value: "0" } })
  })

  it("edits a per series threshold band and hides it on panels without threshold colours", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    // 折线只按序列色渲染，逐序列阈值这一项不该出现。
    await user.click(screen.getByRole("button", { name: "Add override" }))
    expect(screen.queryByLabelText("Thresholds 1 1")).not.toBeInTheDocument()
    await choose(user, "Visualization", "Stat")
    await choose(user, "Match by 1", "Series ID")
    fireEvent.change(screen.getByLabelText("Match value 1"), { target: { value: "A" } })
    await user.click(screen.getByRole("button", { name: "Thresholds 1 Add threshold" }))
    fireEvent.change(screen.getByLabelText("Thresholds 1 1"), { target: { value: "90" } })
    await choose(user, "Thresholds 1 Colour 1", "Rose")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_ref_id", value: "A" }, color: "green", thresholds: [{ value: 90, color: "rose" }] },
    ] }))
    apply.mockClear()
    // 切到不按阈值取色的饼图：阈值编辑器消失，该属性也要从保存结果里丢掉。
    await choose(user, "Visualization", "Pie chart")
    expect(screen.queryByLabelText("Thresholds 1 1")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_ref_id", value: "A" }, color: "green" },
    ] }))
  })

  it("edits the fill below target and drops it when the panel starts stacking", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add override" }))
    fireEvent.change(screen.getByLabelText("Fill below to 1"), { target: { value: "Memory usage" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_regex", value: "^node-" }, color: "green", fill_below_to: "Memory usage" },
    ] }))
    apply.mockClear()
    // 打开堆叠：这一项消失，保存结果里的填充目标也要一起丢掉，避免存出渲染器忽略的配置。
    await choose(user, "Stack series", "Normal")
    expect(screen.queryByLabelText("Fill below to 1")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_regex", value: "^node-" }, color: "green" },
    ] }))
  })

  it("edits a reduce step with the shared value calculation list", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add transformation" }))
    await choose(user, "Step 1", "Reduce")
    await choose(user, "Calculation 1", "Percent change")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ transformations: [
      { kind: "reduce", mode: "reduce_fields", calculations: ["percent_change"] },
    ] }))
  })

  it("edits the per series axis label and soft limits", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add override" }))
    await user.type(screen.getByLabelText("Axis label 1"), "CPU %")
    await user.type(screen.getByLabelText("Soft max 1"), "100")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_regex", value: "^node-" }, color: "green", axis_label: "CPU %", axis_soft_max: 100 },
    ] }))
  })

  it("moves a matched series to the second axis and hides the input on panels without a value axis", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add override" }))
    await choose(user, "Axis placement 1", "Right")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      expect.objectContaining({ matcher: { type: "by_regex", value: "^node-" }, axis_placement: "right" }),
    ] }))
    apply.mockClear()
    // 表格面板没有可切换的值轴：这一项必须消失，切类型时也要把该属性丢掉，避免保存出渲染器忽略的配置。
    await choose(user, "Visualization", "Table")
    expect(screen.queryByRole("combobox", { name: "Axis placement 1" })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    const applied = (apply.mock.calls.at(-1)?.[0] as { overrides?: { axis_placement?: string }[] }).overrides ?? []
    expect(applied.map((override) => override.axis_placement)).toEqual([undefined])
  })

  it("edits a group by step with the identity field and the shared aggregation list", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add transformation" }))
    await choose(user, "Step 1", "Group by")
    expect(screen.getByLabelText("Group by field 1")).toHaveTextContent("Metric")
    await choose(user, "Group by field 1", "Node")
    // 多个计算方式各生成一条合并序列；标签可另起名字，避免只能叫分组键。
    await user.click(screen.getByRole("checkbox", { name: "Maximum" }))
    await user.type(screen.getByLabelText("Alias 1"), "Cluster CPU")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ transformations: [
      { kind: "group_by", group_by: "node_id", calculation: undefined, calculations: ["sum", "max"], alias: "Cluster CPU" },
    ] }))
  })

  it("edits the per series line width and fill opacity and refuses an out of range width", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Add override" }))
    await choose(user, "Match by 1", "Series name")
    fireEvent.change(screen.getByLabelText("Match value 1"), { target: { value: "CPU" } })
    fireEvent.change(screen.getByLabelText("Line width 1"), { target: { value: "4" } })
    fireEvent.change(screen.getByLabelText("Fill opacity 1"), { target: { value: "0" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ overrides: [
      { matcher: { type: "by_name", value: "CPU" }, color: "green", line_width: 4, fill_opacity: 0 },
    ] }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Line width 1"), { target: { value: "9" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findAllByText("An override line width must be a whole number between 1 and 4.")).length).toBeGreaterThan(0)
    expect(apply).not.toHaveBeenCalled()
  })

  it("stores Grafana-style line styling and hides it on non-timeseries panels", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.getByText("Line style")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Line width"), { target: { value: "4" } })
    fireEvent.change(screen.getByLabelText("Fill opacity"), { target: { value: "25" } })
    await choose(user, "Show points", "Always")
    await choose(user, "Stack series", "Normal")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ line_width: 4, fill_opacity: 25, show_points: "always", stack: "normal" }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Line width"), { target: { value: "9" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect((await screen.findAllByText("Line width must be between 1 and 4.")).length).toBeGreaterThan(0)
    expect(apply).not.toHaveBeenCalled()
    apply.mockClear()
    await choose(user, "Visualization", "Table")
    expect(screen.queryByText("Line style")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "table", line_width: undefined, stack: undefined }))
  })

  it("stores the table layout and clears it when the type changes", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={{ ...dashboard.definition.panels[0], type: "table" }} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await choose(user, "Table layout", "One column per series")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "table", table_orientation: "wide" }))
    apply.mockClear()
    await choose(user, "Visualization", "Time series")
    expect(screen.queryByRole("combobox", { name: "Table layout" })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "timeseries", table_orientation: undefined }))
  })

  it("stores pie chart options and clears them when the type changes", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={{ ...dashboard.definition.panels[0], type: "piechart" }} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.queryByText("Bar gauge")).not.toBeInTheDocument()
    await choose(user, "Pie type", "Donut")
    await choose(user, "Slice labels", "Value and percent")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "piechart", pie_chart_type: "donut", pie_label_mode: "both" }))
    apply.mockClear()
    await choose(user, "Visualization", "Table")
    expect(screen.queryByRole("combobox", { name: "Pie type" })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "table", pie_chart_type: undefined, pie_label_mode: undefined }))
  })

  it("stores bar gauge options and clears them when the type changes", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={{ ...dashboard.definition.panels[0], type: "bargauge" }} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.queryByText("Line style")).not.toBeInTheDocument()
    await choose(user, "Orientation", "Horizontal")
    await choose(user, "Display mode", "LCD")
    await user.click(screen.getByRole("switch", { name: "Show unfilled bar" }))
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "bargauge", bar_orientation: "horizontal", bar_display: "lcd", bar_unfilled: false }))
    apply.mockClear()
    await choose(user, "Visualization", "Gauge")
    expect(screen.queryByRole("combobox", { name: "Orientation" })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ type: "gauge", bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined }))
  })

  it("preserves gauge style and supports editing and resetting thresholds", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={{ ...dashboard.definition.panels[0], type: "gauge" }} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await choose(user, "Panel width", "Full width")
    fireEvent.change(screen.getByLabelText("Decimal places"), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("Axis minimum"), { target: { value: "0" } })
    fireEvent.change(screen.getByLabelText("Axis maximum"), { target: { value: "100" } })
    await user.click(screen.getByRole("switch", { name: "Show legend" }))
    await user.click(screen.getByRole("button", { name: "Add threshold" }))
    fireEvent.change(screen.getByRole("spinbutton", { name: "Threshold value 1" }), { target: { value: "90" } })
    await choose(user, "Color 1", "Rose")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ thresholds: [{ value: 90, color: "rose" }] }))
    await user.click(screen.getByRole("button", { name: "Remove threshold" }))
    await user.click(screen.getByRole("button", { name: "Add threshold" }))
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ type: "gauge", size: "large", min: 0, max: 100, decimals: 2, legend: false, thresholds: [{ value: 80, color: "amber" }] }))
  })

  it("stores a relative panel time override and refuses a reversed window", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.getByLabelText("Max data points")).toHaveAttribute("placeholder", "600")
    fireEvent.change(screen.getByLabelText("Start"), { target: { value: "now-6h" } })
    fireEvent.change(screen.getByLabelText("Time shift"), { target: { value: "1d" } })
    fireEvent.change(screen.getByLabelText("Max data points"), { target: { value: "120" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ time_from: "now-6h", time_shift: "1d", max_points: 120 }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Max data points"), { target: { value: "1" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("between 2 and 600")
    expect(apply).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Max data points"), { target: { value: "120" } })
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Time shift"), { target: { value: "1w" } })
    expect(await screen.findByText(/between 1 second and 31 days/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Time shift"), { target: { value: "1d" } })
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("End (optional)"), { target: { value: "now-7h" } })
    expect(await screen.findByText(/keep the window between 1 minute/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).not.toHaveBeenCalled()
  })

  it("renames variable references and refuses a reserved name", async () => {
    const user = userEvent.setup()
    const apply = vi.fn()
    render(<LocaleProvider><WorkspaceDefinitionEditor definition={workspaceDashboard().definition} close={vi.fn()} apply={apply} /></LocaleProvider>)
    fireEvent.change(screen.getByLabelText("Variable name 1"), { target: { value: "constructor" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("unique variable name")
    fireEvent.change(screen.getByLabelText("Variable name 1"), { target: { value: "host" } })
    fireEvent.change(screen.getByLabelText("Display label 1"), { target: { value: "Host" } })
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Production view" } })
    await user.click(screen.getByRole("button", { name: "Add variable" }))
    await choose(user, "Variable type 2", "Node")
    await user.click(screen.getAllByRole("button", { name: "Remove variable" })[1])
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ description: "Production view", variables: [{ name: "host", type: "node", label: "Host" }], panels: [expect.objectContaining({ series: [expect.objectContaining({ node_id: "$host" })] })] }))
  })

  it("stores dashboard search tags and blocks unusable ones", async () => {
    const user = userEvent.setup()
    const apply = vi.fn()
    render(<LocaleProvider><WorkspaceDefinitionEditor definition={workspaceDashboard().definition} close={vi.fn()} apply={apply} /></LocaleProvider>)
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "Prod, hk-1" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ tags: ["prod", "hk-1"] }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "prod, prod" } })
    expect(await screen.findByText("Tags must be unique.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).not.toHaveBeenCalled()
  })

  it("stores dashboard query defaults and rejects values outside the query budget", async () => {
    const user = userEvent.setup()
    const apply = vi.fn()
    render(<LocaleProvider><WorkspaceDefinitionEditor definition={workspaceDashboard().definition} close={vi.fn()} apply={apply} /></LocaleProvider>)
    fireEvent.change(screen.getByLabelText("Max data points"), { target: { value: "240" } })
    fireEvent.change(screen.getByLabelText("Now delay (seconds)"), { target: { value: "30" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ max_points: 240, now_delay_seconds: 30 }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Now delay (seconds)"), { target: { value: "3601" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("between 0 and 3600")
    expect(apply).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Max data points"), { target: { value: "601" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("between 2 and 600")
    expect(apply).not.toHaveBeenCalled()
  })

  it("saves multi-select variables with an option filter and rejects an unusable filter", async () => {
    const user = userEvent.setup()
    const apply = vi.fn()
    render(<LocaleProvider><WorkspaceDefinitionEditor definition={workspaceDashboard().definition} close={vi.fn()} apply={apply} /></LocaleProvider>)
    await user.click(screen.getByRole("checkbox", { name: "Allow multiple values" }))
    await user.click(screen.getByRole("checkbox", { name: "Include All option" }))
    fireEvent.change(screen.getByLabelText("Option filter (RE2) 1"), { target: { value: "^hk-[0-9]+$" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ variables: [{ name: "node", type: "node", label: "Node", multi: true, include_all: true, regex: "^hk-[0-9]+$" }] }))
    apply.mockClear()
    fireEvent.change(screen.getByLabelText("Option filter (RE2) 1"), { target: { value: "(" } })
    expect(await screen.findByText("The option filter is not a valid RE2 expression.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).not.toHaveBeenCalled()
  })

  it("validates custom UTC windows and supports pause and manual refresh", async () => {
    const user = userEvent.setup()
    const onRange = vi.fn(), onInterval = vi.fn(), onRefresh = vi.fn()
    render(<LocaleProvider><WorkspaceTimeControls range={{ mode: "relative", seconds: 3600 }} refreshSeconds={15} onRange={onRange} onInterval={onInterval} onRefresh={onRefresh} /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Pause refresh" }))
    expect(onInterval).toHaveBeenCalledWith(0)
    await choose(user, "Auto refresh", "Every 30s")
    expect(onInterval).toHaveBeenCalledWith(30)
    await user.click(screen.getByRole("button", { name: "Refresh now" }))
    expect(onRefresh).toHaveBeenCalledOnce()
    await user.click(screen.getByRole("button", { name: "Custom range" }))
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-13T02:00" } })
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-09-13T01:00" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("valid time range")
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-13T00:00" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(onRange).toHaveBeenCalledWith({ mode: "absolute", from: "2026-09-13T00:00:00.000Z", to: "2026-09-13T01:00:00.000Z" })
  })

  it("collects legend values, order and tooltip and clears them with the legend", { timeout: 20000 }, async () => {
    const user = userEvent.setup(), apply = vi.fn(), dashboard = workspaceDashboard()
    render(<LocaleProvider><WorkspacePanelEditor panel={dashboard.definition.panels[0]} definition={dashboard.definition}
      metrics={[workspaceMetric()]} nodes={[workspaceNode()]} close={vi.fn()} apply={apply} /></LocaleProvider>)
    expect(screen.getByRole("switch", { name: "Last" })).toBeEnabled()
    await user.click(screen.getByRole("switch", { name: "Last" }))
    await choose(user, "Legend order", "Highest first")
    await choose(user, "Tooltip", "Hovered series")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ legend_values: ["last"], legend_sort: "desc", tooltip: "single" }))
    apply.mockClear()
    await user.click(screen.getByRole("switch", { name: "Show legend" }))
    expect(screen.getByRole("switch", { name: "Last" })).toHaveAttribute("aria-disabled", "true")
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ legend: false, legend_values: [], legend_sort: "none", tooltip: "single" }))
  })
})
