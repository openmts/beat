import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { WorkspaceToolbar } from "./toolbar"
import type { WorkspaceEditor } from "@/hooks/use-workspace-editor"
import type { DashboardDefinition, MetricDashboard } from "@/types/metric-workspace"

vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})

const definition: DashboardDefinition = { version: 1, title: "Fleet", description: "", time_range_seconds: 3600, refresh_seconds: 15,
  variables: [{ name: "node", type: "node", label: "Node" }], panels: [] }
const record: MetricDashboard = { id: "admin-default", scope: "admin", revision: 1, has_previous: false, created_at: "", updated_at: "", definition }

function editor(): WorkspaceEditor {
  return { record, saved: record, dashboards: [record], busy: false, dirty: false, conflict: false } as unknown as WorkspaceEditor
}

describe("workspace toolbar portability", () => {
  it("hands selected files to the import handler", () => {
    const importFile = vi.fn()
    const { container } = render(<WorkspaceToolbar editor={editor()} owner={false} select={vi.fn()} action={vi.fn()} importFile={importFile} />)
    const input = container.querySelector("input[type=file]") as HTMLInputElement
    const file = new File(["{}"], "dashboard.json", { type: "application/json" })
    fireEvent.change(input, { target: { files: [file] } })
    expect(importFile).toHaveBeenCalledWith(file)
    expect(input.value).toBe("")
  })
  it("routes export, import and share through the dashboard menu", async () => {
    const user = userEvent.setup(), action = vi.fn(), importFile = vi.fn()
    render(<WorkspaceToolbar editor={editor()} owner={false} select={vi.fn()} action={action} importFile={importFile} />)
    await user.click(screen.getByRole("button", { name: "Dashboard" }))
    await user.click(await screen.findByRole("menuitem", { name: "Export dashboard JSON" }))
    expect(action).toHaveBeenCalledWith("export")
    await user.click(screen.getByRole("button", { name: "Dashboard" }))
    await user.click(await screen.findByRole("menuitem", { name: "Import dashboard JSON" }))
    expect(importFile).not.toHaveBeenCalled()
    await user.click(screen.getByRole("button", { name: "Dashboard" }))
    await user.click(await screen.findByRole("menuitem", { name: "Share dashboard" }))
    expect(action).toHaveBeenCalledWith("share")
    await user.click(screen.getByRole("button", { name: "Dashboard" }))
    await user.click(await screen.findByRole("menuitem", { name: "Duplicate dashboard" }))
    expect(action).toHaveBeenCalledWith("duplicate")
  })
  it("filters the dashboard list by title and tags but keeps the current selection", async () => {
    const user = userEvent.setup()
    const select = vi.fn()
    const staging: MetricDashboard = { ...record, id: "personal-1", scope: "personal",
      definition: { ...definition, title: "Staging", tags: ["staging"] } }
    const operations: MetricDashboard = { ...record, id: "personal-2", scope: "personal",
      definition: { ...definition, title: "Operations" } }
    render(<WorkspaceToolbar editor={{ ...editor(), dashboards: [record, staging, operations] }} owner={false} select={select} action={vi.fn()} importFile={vi.fn()} />)
    await user.click(screen.getByRole("combobox", { name: "Dashboard" }))
    expect(await screen.findByRole("option", { name: /Operations/ })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Search dashboards"), { target: { value: "Stag" } })
    expect(screen.queryByRole("option", { name: /Operations/ })).not.toBeInTheDocument()
    expect(screen.getByRole("option", { name: /Fleet/ })).toBeInTheDocument()
    await user.click(screen.getByRole("option", { name: /Staging/ }))
    expect(select).toHaveBeenCalledWith("personal-1")
  })

  it("renders dashboard actions for the current selection", () => {
    render(<WorkspaceToolbar editor={editor()} owner={false} select={vi.fn()} action={vi.fn()} importFile={vi.fn()} />)
    expect(screen.getByRole("button", { name: "Save a copy" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "New dashboard" })).toBeEnabled()
  })
})
