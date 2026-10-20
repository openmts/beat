import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { importWorkspaceFile, workspaceToolbarAction } from "./workspace-page-actions"
import { exportDashboardJSON } from "./workspace-portable"
import type { WorkspacePage } from "@/hooks/use-workspace-page"
import type { DashboardDefinition } from "@/types/metric-workspace"

const definition: DashboardDefinition = { version: 1, title: "Fleet", description: "", time_range_seconds: 3600, refresh_seconds: 15,
  variables: [{ name: "node", type: "node", label: "Node" }],
  panels: [{ id: "cpu", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [],
    series: [{ ref_id: "a", node_id: "$node", metric: "cpu.usage", aggregation: "avg", color: "blue", label: "CPU" }] }] }

function fakePage() {
  const adopt = vi.fn(), setError = vi.fn(), setLink = vi.fn()
  const page = { editor: { adopt, record: { id: "dash-1", definition, scope: "personal" } }, values: { node: ["node-1"] }, setError, setLink }
  return { page: page as unknown as WorkspacePage, context: { page: page as unknown as WorkspacePage, sources: {} as never, t: (key: string) => key }, adopt, setError, setLink }
}

// 分享链接要用真实 URL 类拼查询串，这里只替掉 blob 两个方法。
beforeEach(() => {
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:dashboard")
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

describe("workspace portability actions", () => {
  it("shares the selected dashboard with its node variable", () => {
    const harness = fakePage()
    workspaceToolbarAction("share", harness.context)
    const link = harness.setLink.mock.calls[0][0] as string
    expect(link).toContain("dashboard=dash-1")
    expect(link).toContain("node_id=node-1")
    expect(link).toContain("var-node=node-1")
  })
  it("exports the current definition as a download", () => {
    const harness = fakePage()
    workspaceToolbarAction("export", harness.context)
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
    expect(harness.setError).not.toHaveBeenCalled()
  })
  it("reports a failed export", () => {
    vi.mocked(URL.createObjectURL).mockImplementation(() => { throw new Error("blocked") })
    const harness = fakePage()
    workspaceToolbarAction("export", harness.context)
    expect(harness.setError).toHaveBeenCalledWith("workspace.export_failed")
  })
  it("loads imported files into an unsaved draft", async () => {
    const harness = fakePage()
    await importWorkspaceFile(harness.page, { text: async () => exportDashboardJSON(definition) } as File)
    expect(harness.adopt).toHaveBeenCalledWith(definition)
    expect(harness.setError).not.toHaveBeenCalled()
  })
  it("reports invalid imports", async () => {
    const harness = fakePage()
    await importWorkspaceFile(harness.page, { text: async () => "{not json" } as File)
    expect(harness.adopt).not.toHaveBeenCalled()
    expect(harness.setError).toHaveBeenCalledWith("workspace.import_invalid")
  })
})
