import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspacePanel } from "./panel"

const panel = { ...workspaceDashboard().definition.panels[0], title: "CPU usage" }
const ready = { loading: false, result: workspaceResult() }

describe("workspace panel header", () => {
  it("opens the maximized panel view from the header icon", async () => {
    const user = userEvent.setup()
    render(<LocaleProvider><WorkspacePanel readOnly panel={panel} state={ready} inspect={vi.fn()} onRange={vi.fn()} retry={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("button", { name: "Inspect data · CPU usage" })).toBeInTheDocument()
    const view = screen.getByRole("button", { name: "View panel · CPU usage" })
    await user.click(view)
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    expect(screen.getAllByText("CPU usage").length).toBeGreaterThan(1)
  })

  it("shows a per-panel retry action when the query failed", async () => {
    const user = userEvent.setup()
    const retry = vi.fn()
    render(<LocaleProvider><WorkspacePanel panel={panel} state={{ loading: false, error: "workspace.query_failed" }} index={0} count={1} busy={false} action={vi.fn()} drop={vi.fn()} inspect={vi.fn()} onRange={vi.fn()} retry={retry} /></LocaleProvider>)
    const alert = screen.getByRole("alert")
    expect(alert).toHaveTextContent("The metric query failed.")
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(retry).toHaveBeenCalledTimes(1)
    expect(screen.getByRole("button", { name: "Edit panel · CPU usage" })).toBeInTheDocument()
  })

  it("hides the retry action when no retry handler is wired", () => {
    render(<LocaleProvider><WorkspacePanel readOnly panel={panel} state={{ loading: false, error: "workspace.query_failed" }} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument()
  })

  it("shows the panel time badge only while the panel overrides time options", () => {
    const { rerender } = render(<LocaleProvider><WorkspacePanel readOnly panel={panel} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.queryByTitle("Query options")).not.toBeInTheDocument()
    rerender(<LocaleProvider><WorkspacePanel readOnly panel={{ ...panel, time_from: "now-6h" }} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByTitle("Query options")).toHaveTextContent("now-6h")
    rerender(<LocaleProvider><WorkspacePanel readOnly panel={{ ...panel, time_shift: "1d" }} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByTitle("Query options")).toHaveTextContent("↩ 1d")
  })

  it("links to the prefilled alert rule page from the panel menu", async () => {
    const user = userEvent.setup()
    render(<LocaleProvider><WorkspacePanel panel={panel} state={ready} index={0} count={1} busy={false} action={vi.fn()} drop={vi.fn()} inspect={vi.fn()} onRange={vi.fn()} alertLink="/admin/alerts?tab=rules&metric=cpu" /></LocaleProvider>)
    await user.click(screen.getByRole("button", { name: "Edit panel · CPU usage" }))
    const item = await screen.findByRole("menuitem", { name: "Create alert rule" })
    expect(item).toHaveAttribute("href", "/admin/alerts?tab=rules&metric=cpu")
    expect(screen.getByRole("menuitem", { name: "Edit panel" })).toBeInTheDocument()
  })

  it("omits the alert entry for read-only panels", () => {
    render(<LocaleProvider><WorkspacePanel readOnly panel={panel} state={ready} inspect={vi.fn()} onRange={vi.fn()} alertLink="/admin/alerts?tab=rules&metric=cpu" /></LocaleProvider>)
    expect(screen.queryByRole("button", { name: "Edit panel · CPU usage" })).not.toBeInTheDocument()
    expect(screen.queryByRole("menuitem", { name: "Create alert rule" })).not.toBeInTheDocument()
  })
})

describe("read-only link hiding", () => {
  const linked = { ...panel, links: [{ title: "Runbook", url: "/admin/metrics?node=${node}" }] }

  it("keeps panel data links visible while editing", () => {
    render(<LocaleProvider><WorkspacePanel panel={linked} state={ready} index={0} count={1} busy={false} action={vi.fn()} drop={vi.fn()} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("link", { name: "Runbook" })).toBeInTheDocument()
  })

  it("drops them in the read-only view when the dashboard hides links", () => {
    const { rerender } = render(<LocaleProvider><WorkspacePanel readOnly panel={linked} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("link", { name: "Runbook" })).toBeInTheDocument()
    rerender(<LocaleProvider><WorkspacePanel readOnly hideLinks panel={linked} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.queryByRole("link", { name: "Runbook" })).not.toBeInTheDocument()
  })

  it("drops text-panel links as well", () => {
    const text = "值班说明"
    const { rerender } = render(<LocaleProvider><WorkspacePanel readOnly panel={{ ...linked, type: "text", series: [], text }} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("link", { name: "Runbook" })).toBeInTheDocument()
    rerender(<LocaleProvider><WorkspacePanel readOnly hideLinks panel={{ ...linked, type: "text", series: [], text }} state={ready} inspect={vi.fn()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.queryByRole("link", { name: "Runbook" })).not.toBeInTheDocument()
    expect(screen.getByText("值班说明")).toBeInTheDocument()
  })
})
