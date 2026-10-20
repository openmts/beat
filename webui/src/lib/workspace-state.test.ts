import { describe, expect, it } from "vitest"
import { applyDashboardSave, blankWorkspaceDashboard, moveWorkspacePanel, replaceWorkspacePanel, workspaceDirty } from "@/lib/workspace-editor"
import { additionalWorkspaceSeries, newWorkspacePanel, validateWorkspacePanel } from "@/lib/workspace-panel-editor"
import { applyWorkspaceVariables } from "@/lib/workspace-variables"
import { chartTimestamp, latestWorkspaceValue, mergeWorkspaceSeries, workspaceSeriesLabel, workspaceThresholdColor } from "@/lib/workspace-chart"
import { workspaceDashboard, workspaceMetric, workspaceResult } from "@/test/metric-workspace-fixtures"

describe("workspace state boundaries", () => {
  it("preserves edits made while a save was in flight", () => {
    const captured = workspaceDashboard()
    const current = structuredClone(captured)
    current.definition.title = "Newer edit"
    const state = { record: current, saved: captured, dashboards: [captured], busy: true, conflict: false }
    const response = { ...captured, revision: 1 }
    const next = applyDashboardSave(state, response, captured)
    expect(next.record.definition.title).toBe("Newer edit")
    expect(next.saved.definition.title).toBe(captured.definition.title)
    expect(workspaceDirty(next)).toBe(true)
    expect(applyDashboardSave({ ...state, record: { ...current, id: "different" } }, response, captured).record.id).toBe("different")
    expect(workspaceDirty({ ...state, record: blankWorkspaceDashboard("Blank") })).toBe(true)
  })

  it("reorders by stable identity and enforces the panel budget", () => {
    const definition = workspaceDashboard().definition
    const second = { ...definition.panels[0], id: "second" }
    const added = replaceWorkspacePanel(definition, second)
    expect(moveWorkspacePanel(added, "second", 0).panels[0].id).toBe("second")
    expect(moveWorkspacePanel(added, "missing", 0)).toBe(added)
    expect(moveWorkspacePanel(added, "second", 100)).toBe(added)
    expect(replaceWorkspacePanel(added, { ...second, title: "Updated" }).panels[1].title).toBe("Updated")
    expect(() => replaceWorkspacePanel({ ...definition, panels: Array.from({ length: 24 }, (_, i) => ({ ...second, id: String(i) })) }, second)).toThrow("workspace.panel_limit")
  })

  it("validates units, finite thresholds, bounds and single-value views", () => {
    const definition = workspaceDashboard().definition
    const metrics = [workspaceMetric(), workspaceMetric("memory", "bytes")]
    const panel = newWorkspacePanel(metrics, definition)
    expect(validateWorkspacePanel(panel, metrics)).toBeNull()
    const second = additionalWorkspaceSeries(panel)
    expect(second.ref_id).toBe("B")
    expect(validateWorkspacePanel({ ...panel, title: "" }, metrics)).toBe("workspace.invalid_panel")
    expect(validateWorkspacePanel({ ...panel, series: [...panel.series, { ...second, metric: "memory" }] }, metrics)).toBe("workspace.unit_conflict")
    expect(validateWorkspacePanel({ ...panel, unit: "bytes" }, metrics)).toBe("workspace.unit_conflict")
    expect(validateWorkspacePanel({ ...panel, type: "gauge", series: [...panel.series, second] }, metrics)).toBe("workspace.single_series")
    for (const change of [{ min: 2, max: 1 }, { min: Infinity }, { decimals: 7 }, { thresholds: [{ value: NaN, color: "blue" as const }] }]) expect(validateWorkspacePanel({ ...panel, ...change }, metrics)).toBe("workspace.invalid_panel")
    expect(validateWorkspacePanel({ ...panel, line_width: 3, fill_opacity: 20, show_points: "always", stack: "normal" }, metrics)).toBeNull()
    expect(validateWorkspacePanel({ ...panel, line_width: 9 }, metrics)).toBe("workspace.line_width_invalid")
    expect(validateWorkspacePanel({ ...panel, type: "table", stack: "normal" }, metrics)).toBe("workspace.line_style_timeseries_only")
    expect(validateWorkspacePanel({ ...panel, series: [{ ...panel.series[0], metric: "missing" }] }, metrics)).toBe("workspace.invalid_panel")
  })

  it("renames variables atomically and rejects deletion of referenced variables", () => {
    const definition = workspaceDashboard().definition
    const updated = applyWorkspaceVariables(definition, [{ id: "v", original: "node", value: { name: "host", type: "node", label: "Host" } }])
    expect(updated.panels[0].series[0].node_id).toBe("$host")
    expect(definition.panels[0].series[0].node_id).toBe("$node")
    expect(() => applyWorkspaceVariables(definition, [])).toThrow("workspace.variable_in_use")
    expect(() => applyWorkspaceVariables(definition, [{ id: "v", value: { name: "constructor", type: "node", label: "Host" } }])).toThrow("workspace.invalid_variable")
  })

  it("keeps null gaps and the actual last sample while selecting threshold colors", () => {
    const result = workspaceResult()
    expect(mergeWorkspaceSeries(result)[1].A).toBeNull()
    expect(latestWorkspaceValue(result.series[0])).toBe(40)
    result.series[0].points = []
    expect(latestWorkspaceValue(result.series[0])).toBeNull()
    expect(workspaceSeriesLabel(result.series[0])).toBe("CPU usage")
    const panel = workspaceDashboard().definition.panels[0]
    panel.thresholds = [{ value: 50, color: "amber" }, { value: 80, color: "rose" }]
    expect(workspaceThresholdColor(panel, 90)).toBe("#e11d48")
    expect(workspaceThresholdColor(panel, null)).toBe("#3b82f6")
    expect(chartTimestamp(null)).toBeNull()
    expect(chartTimestamp("")).toBeNull()
    expect(chartTimestamp("invalid")).toBeNull()
    expect(chartTimestamp("1000")).toBe(1000)
  })
})
