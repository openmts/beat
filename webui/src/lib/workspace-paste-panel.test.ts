import { describe, expect, it } from "vitest"
import { maximumPastedPanelBytes, maximumWorkspacePanels, parsePastedPanelJSON } from "@/lib/workspace-paste-panel"
import { workspaceDashboard, workspaceMetric } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel } from "@/types/metric-workspace"

const metrics = [workspaceMetric()]
const definition = workspaceDashboard().definition
const series = { ref_id: "A", node_id: "$node", metric: "cpu", aggregation: "avg", label: "", color: "blue" } as const
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

function payload(overrides: Partial<DashboardPanel> = {}): string {
  return JSON.stringify({ id: "pasted", title: "Pasted panel", type: "timeseries", size: "medium", legend: true, tooltip: "all",
    unit: "auto", thresholds: [], series: [series], ...overrides })
}

function errorOf(text: string, panels = definition.panels): string | undefined {
  const result = parsePastedPanelJSON(text, { ...definition, panels }, metrics)
  return "error" in result ? result.error : undefined
}

describe("workspace pasted panel JSON", () => {
  it("keeps a free panel id and returns the pasted definition", () => {
    const result = parsePastedPanelJSON(payload(), definition, metrics)
    expect("error" in result ? result.error : result.panel.id).toBe("pasted")
  })

  it("replaces colliding or unusable ids with a fresh uuid", () => {
    for (const id of ["cpu", "bad id!", ""]) {
      const result = parsePastedPanelJSON(payload({ id }), definition, metrics)
      const value = "error" in result ? result.error : result.panel.id
      expect(value).not.toBe("cpu")
      expect(uuid.test(value) || /^[a-f0-9-]{36}$/.test(value)).toBe(true)
    }
  })

  it("defaults missing collections instead of failing on absent fields", () => {
    const result = parsePastedPanelJSON(JSON.stringify({ type: "row", title: "Row", row_collapsed: true }), definition, metrics)
    const panel = "error" in result ? null : result.panel
    expect([panel?.series.length, panel?.thresholds.length]).toEqual([0, 0])
  })

  it("rejects empty, malformed and non-panel payloads", () => {
    expect(errorOf("   ")).toBe("workspace.paste_empty")
    expect(errorOf("{oops")).toBe("workspace.paste_invalid")
    expect(errorOf("[1,2]")).toBe("workspace.paste_invalid")
    expect(errorOf('{"title":"Pasted"}')).toBe("workspace.paste_invalid")
    expect(errorOf("null")).toBe("workspace.paste_invalid")
  })

  it("rejects oversized payloads and dashboards at the panel limit", () => {
    expect(errorOf("a".repeat(maximumPastedPanelBytes + 1))).toBe("workspace.paste_too_large")
    const full = Array.from({ length: maximumWorkspacePanels }, (_, index) => ({ ...definition.panels[0], id: `panel-${index}` }))
    expect(errorOf(payload(), full)).toBe("workspace.panel_limit")
  })

  it("surfaces the shared panel validation errors", () => {
    expect(errorOf(payload({ type: "stat", series: [series, { ...series, ref_id: "B" }] }))).toBe("workspace.single_series")
    expect(errorOf(payload({ series: [{ ...series, metric: "missing" }] }))).toBe("workspace.invalid_panel")
    expect(errorOf(payload({ title: "" }))).toBe("workspace.invalid_panel")
  })
})
